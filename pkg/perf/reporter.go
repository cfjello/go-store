package perf

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	reporterMu     sync.Mutex
	reporterStop   chan struct{}
	reporterDone   chan struct{}
	reporterActive bool
)

// StartReporter starts a background ticker that logs the top functions by
// total time and by call count. A no-op when monitoring is disabled or
// PERF_REPORT_INTERVAL_MS is 0. Safe to call more than once; a second call
// is ignored while a reporter is already running. The returned stop function
// ends the ticker (and is a no-op when nothing was started).
func StartReporter() (stop func()) {
	if !Enabled() {
		return func() {}
	}
	interval := reportInterval()
	if interval <= 0 {
		return func() {}
	}
	n := reportTopN()

	reporterMu.Lock()
	defer reporterMu.Unlock()
	if reporterActive {
		return stopReporter
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	reporterStop = stopCh
	reporterDone = doneCh
	reporterActive = true
	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logTop(n)
			case <-stopCh:
				return
			}
		}
	}()
	return stopReporter
}

func stopReporter() {
	reporterMu.Lock()
	if !reporterActive {
		reporterMu.Unlock()
		return
	}
	stopCh := reporterStop
	doneCh := reporterDone
	reporterActive = false
	reporterStop = nil
	reporterDone = nil
	reporterMu.Unlock()
	close(stopCh)
	<-doneCh
}

func logTop(n int) {
	byTotal := Top(n, ByTotal)
	byCalls := Top(n, ByCalls)
	log.Printf("perf: top by total: %s", formatStats(byTotal))
	log.Printf("perf: top by calls: %s", formatStats(byCalls))
}

func formatStats(stats []Stat) string {
	if len(stats) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for i, s := range stats {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(s.Name)
		b.WriteString(" calls=")
		b.WriteString(strconv.FormatInt(s.Calls, 10))
		b.WriteString(" total=")
		b.WriteString(s.Total.String())
		b.WriteString(" min=")
		b.WriteString(s.Min.String())
		b.WriteString(" max=")
		b.WriteString(s.Max.String())
		b.WriteString(" last=")
		b.WriteString(s.Last.String())
	}
	return b.String()
}

// Settings is a point-in-time snapshot of the perf configuration: whether
// recording is enabled and the reporter's interval and list size.
type Settings struct {
	Enabled          bool `json:"enabled"`
	ReportIntervalMs int  `json:"reportIntervalMs"`
	ReportTopN       int  `json:"reportTopN"`
}

// CurrentSettings returns the active perf settings for the GET /perf/config
// endpoint and similar monitoring surfaces.
func CurrentSettings() Settings {
	return Settings{
		Enabled:          Enabled(),
		ReportIntervalMs: int(reportInterval() / time.Millisecond),
		ReportTopN:       reportTopN(),
	}
}

func reportInterval() time.Duration {
	ms, err := strconv.Atoi(os.Getenv("PERF_REPORT_INTERVAL_MS"))
	if err != nil {
		return 60 * time.Second
	}
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func reportTopN() int {
	n, err := strconv.Atoi(os.Getenv("PERF_REPORT_TOP_N"))
	if err != nil || n < 1 {
		return 10
	}
	return n
}
