package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
)

// --- Configuration ---
type Config struct {
	Server   ServerConfig    `yaml:"server"`
	Stations []StationConfig `yaml:"stations"`
}
type ServerConfig struct {
	Port int `yaml:"port"`
}
type StationConfig struct {
	Name string `yaml:"name"`
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// --- Constants ---
const (
	SyncChar1 = '$'
	SyncChar2 = '@'

	// Block IDs
	BlockID_PVTGeodetic    = 4007
	BlockID_ReceiverStatus = 4014 // CPU, Uptime, Temp
	BlockID_MeasEpoch      = 4027
	BlockID_DiskStatus     = 4059 // Disk Space
	BlockID_QualityInd     = 4082 // Quality Indicators (0-10)
	BlockID_RFStatus       = 4092
)

// --- Metrics ---
var (
	// Existing
	satellitesTracked = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_satellites_tracked_total", Help: "Satellites visible"}, []string{"station"})
	satellitesUsed    = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_satellites_used_total", Help: "Satellites used in solution"}, []string{"station"})
	jammingStatus     = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_jamming_status_code", Help: "0=None, 1=Warning, 2=Critical"}, []string{"station"})
	receiverConnected = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_receiver_connected", Help: "Connection status"}, []string{"station"})

	// NEW: System Health
	cpuLoad     = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_cpu_load_percent", Help: "CPU Load (0-100)"}, []string{"station"})
	temperature = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_temperature_celsius", Help: "Internal Temperature"}, []string{"station"})
	uptime      = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_uptime_seconds", Help: "Receiver Uptime"}, []string{"station"})
	diskFree    = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_disk_free_bytes", Help: "Free internal disk space"}, []string{"station"})

	// NEW: Quality Indicators (0-10 scale)
	qualityOverall = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_quality_overall", Help: "Overall Quality Indicator (0-10)"}, []string{"station"})
	qualitySignal  = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_quality_signals", Help: "GNSS Signal Quality (0-10)"}, []string{"station"})
	qualityRF      = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gnss_quality_rf", Help: "RF Power Quality (0-10)"}, []string{"station"})
)

func init() {
	prometheus.MustRegister(satellitesTracked, satellitesUsed, jammingStatus, receiverConnected)
	prometheus.MustRegister(cpuLoad, temperature, uptime, diskFree)
	prometheus.MustRegister(qualityOverall, qualitySignal, qualityRF)
}

func main() {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	for _, s := range cfg.Stations {
		go monitorStation(s)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Septentrio Exporter running on %s", addr)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(addr, nil))
}

func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	return &cfg, err
}

func monitorStation(s StationConfig) {
	address := fmt.Sprintf("%s:%d", s.Host, s.Port)
	logger := log.New(log.Writer(), fmt.Sprintf("[%s] ", s.Name), log.LstdFlags)

	for {
		receiverConnected.WithLabelValues(s.Name).Set(0)

		conn, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			logger.Printf("Connection failed. Retrying in 10s...")
			time.Sleep(10 * time.Second)
			continue
		}

		receiverConnected.WithLabelValues(s.Name).Set(1)
		logger.Printf("Connected to %s", s.Name)
		handleStream(conn, s.Name, logger)
		conn.Close()

		logger.Printf("Connection lost. Reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

func handleStream(conn net.Conn, stationName string, log *log.Logger) {
	reader := bufio.NewReader(conn)
	headerBuf := make([]byte, 8)

	for {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if b != SyncChar1 {
			continue
		}
		b, err = reader.ReadByte()
		if err != nil {
			return
		}
		if b != SyncChar2 {
			continue
		}

		if _, err := io.ReadFull(reader, headerBuf[2:]); err != nil {
			return
		}

		idRaw := binary.LittleEndian.Uint16(headerBuf[4:6])
		length := binary.LittleEndian.Uint16(headerBuf[6:8])
		baseID := idRaw & 0x1FFF

		if length < 8 || length > 8192 {
			continue
		}

		payloadLen := int(length) - 8
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}

		parseBlock(stationName, baseID, payload, log)
	}
}

func parseBlock(stationName string, id uint16, payload []byte, log *log.Logger) {
	switch id {
	case BlockID_PVTGeodetic: // 4007
		if len(payload) > 66 {
			nrSv := payload[66]
			satellitesUsed.WithLabelValues(stationName).Set(float64(nrSv))
		}

	case BlockID_MeasEpoch: // 4027
		if len(payload) > 6 {
			n := payload[6]
			satellitesTracked.WithLabelValues(stationName).Set(float64(n))
		}

	case BlockID_RFStatus: // 4092
		if len(payload) > 8 {
			flags := payload[8]
			state := 0.0
			if (flags & 0x01) != 0 {
				state = 1.0
			}
			if (flags & 0x02) != 0 {
				state = 2.0
			}
			jammingStatus.WithLabelValues(stationName).Set(state)
		}

	case BlockID_ReceiverStatus: // 4014
		// Layout: CPULoad(u8, off=6), Uptime(u32, off=7), ... Temp(u8, off=15 typically)
		// Note: Offsets can vary by firmware revision.
		if len(payload) >= 16 {
			// CPU Load (Offset 6)
			cpu := payload[6]
			cpuLoad.WithLabelValues(stationName).Set(float64(cpu))

			// Uptime (Offset 7, u32)
			up := binary.LittleEndian.Uint32(payload[7:11])
			uptime.WithLabelValues(stationName).Set(float64(up))

			// Temperature (Offset 15 often contains temp in Celsius)
			// Note: Some firmwares place it elsewhere. If this reads weird, let me know.
			temp := int8(payload[15])
			temperature.WithLabelValues(stationName).Set(float64(temp))
		}

	case BlockID_QualityInd: // 4082
		// Layout: Overall(u8), GNSS(u8), RF(u8) ... (offsets vary, usually start at 6 after headers)
		// Payload: TOW(4) + WNc(2) + Qualities...
		if len(payload) >= 9 {
			// Offset 6: Overall Quality (0-10)
			qOver := payload[6]
			qualityOverall.WithLabelValues(stationName).Set(float64(qOver))

			// Offset 7: GNSS Signals (0-10)
			qSig := payload[7]
			qualitySignal.WithLabelValues(stationName).Set(float64(qSig))

			// Offset 8: RF Power (0-10)
			qRF := payload[8]
			qualityRF.WithLabelValues(stationName).Set(float64(qRF))
		}

	case BlockID_DiskStatus: // 4059
		// Layout: N(u8), SB(u8), [DiskID, Capacity(u32), Used(u32)]
		// Payload: TOW(4) + WNc(2) + N(1) + SB(1) + [Disk Data...]
		if len(payload) >= 20 {
			// Offset 8: DiskID
			// Offset 9-12: Capacity (MB) - u32
			// Offset 13-16: Used (MB) - u32

			capacityMB := binary.LittleEndian.Uint32(payload[9:13])
			usedMB := binary.LittleEndian.Uint32(payload[13:17])

			freeBytes := float64(capacityMB-usedMB) * 1024 * 1024
			diskFree.WithLabelValues(stationName).Set(freeBytes)
		}
	}
}
