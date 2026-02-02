package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"

	"dnsdist-collector/model"
)

type ClickHouseWriter struct {
	URL           string
	LogChan       <-chan model.DNSLog
	BatchSize     int
	FlushInterval time.Duration
	Done          chan struct{}
	Client        *http.Client
}

func NewClickHouseWriter(httpAddr string, logChan <-chan model.DNSLog) (*ClickHouseWriter, error) {
	// httpAddr must be "ip:8123"
	url := fmt.Sprintf("http://%s/?query=INSERT+INTO+dns.dns_logs+FORMAT+JSONEachRow", httpAddr)

	return &ClickHouseWriter{
		URL:           url,
		LogChan:       logChan,
		BatchSize:     10000,
		FlushInterval: 1 * time.Second,
		Done:          make(chan struct{}),
		Client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     30 * time.Second,
				DisableKeepAlives:   false,
				DisableCompression:  true,
			},
		},
	}, nil
}

func (w *ClickHouseWriter) Worker() {
	defer close(w.Done)

	batch := make([]model.DNSLog, 0, w.BatchSize)
	ticker := time.NewTicker(w.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		// 1 quick retry (short jitter) then drop to avoid long blocking
		if err := w.sendBatch(batch); err != nil {
			j := time.Duration(100+rand.Intn(200)) * time.Millisecond
			time.Sleep(j)

			if err2 := w.sendBatch(batch); err2 != nil {
				log.Printf("ClickHouse insert failed (dropping %d rows): %v", len(batch), err2)
			}
		}

		batch = batch[:0]
	}

	for {
		select {
		case item, ok := <-w.LogChan:
			if !ok {
				// Channel closed: final flush then exit
				flush()
				return
			}
			batch = append(batch, item)
			if len(batch) >= w.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (w *ClickHouseWriter) sendBatch(logs []model.DNSLog) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	// JSONEachRow expects one JSON object per line (NDJSON)
	for _, l := range logs {
		if err := enc.Encode(l); err != nil {
			return err
		}
	}

	req, err := http.NewRequest("POST", w.URL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("clickhouse status=%s body=%q", resp.Status, string(b))
	}

	return nil
}
