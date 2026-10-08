package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func benchmarkRecordApp(b *testing.B, count int) *app {
	b.Helper()
	db, err := openDatabase(filepath.Join(b.TempDir(), "clio.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	a := &app{db: db, project: defaultProject}
	if _, err = db.Exec(`INSERT INTO groups_meta(name,label) VALUES('bench','Benchmark')`); err != nil {
		b.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO tables_meta(group_name,name,label,kind) VALUES('bench','events','Events','record')`); err != nil {
		b.Fatal(err)
	}
	fields := []map[string]any{
		{"name": "category", "label": "Category", "type": "string", "order": 0, "required": false},
		{"name": "amount", "label": "Amount", "type": "integer", "order": 1, "required": false},
		{"name": "note", "label": "Note", "type": "string", "order": 2, "required": false},
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for i, field := range fields {
		definition, marshalErr := json.Marshal(field)
		if marshalErr != nil {
			b.Fatal(marshalErr)
		}
		if _, err = tx.Exec(`INSERT INTO fields_meta(group_name,table_name,name,position,definition) VALUES('bench','events',?,?,?)`, field["name"], i, string(definition)); err != nil {
			b.Fatal(err)
		}
	}
	stmt, err := tx.Prepare(`INSERT INTO records(id,group_name,table_name,created_at,updated_at,data) VALUES(?, 'bench','events',?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		created := formatUTC(base.Add(time.Duration(i) * time.Microsecond))
		data, marshalErr := json.Marshal(map[string]any{
			"category": []string{"service", "repair", "fuel", "other"}[i%4],
			"amount":   i % 10000,
			"note":     "Representative personal record note",
		})
		if marshalErr != nil {
			b.Fatal(marshalErr)
		}
		if _, err = stmt.Exec(fmt.Sprintf("record-%08d", i), created, created, string(data)); err != nil {
			b.Fatal(err)
		}
	}
	if err = stmt.Close(); err != nil {
		b.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return a
}

func BenchmarkQueryRecordsMemory(b *testing.B) {
	for _, count := range []int{10_000, 50_000} {
		a := benchmarkRecordApp(b, count)
		for _, workload := range []struct {
			name string
			q    url.Values
		}{
			{name: "default_page", q: url.Values{"limit": {"100"}}},
			{name: "sorted_page", q: url.Values{"limit": {"100"}, "sort": {"amount"}, "order": {"desc"}}},
			{name: "aggregate", q: url.Values{"aggregate": {"count,amount:avg"}}},
			{name: "distinct", q: url.Values{"distinct": {"category"}}},
			{name: "grouped", q: url.Values{"group_by": {"category"}, "aggregate": {"count,amount:avg"}}},
		} {
			b.Run(fmt.Sprintf("rows_%d/%s", count, workload.name), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, ae := a.queryRecords("bench", "events", workload.q); ae != nil {
						b.Fatal(ae)
					}
				}
			})
		}
	}
}

func BenchmarkQueryRecordsWorkingSet(b *testing.B) {
	for _, count := range []int{10_000, 50_000} {
		a := benchmarkRecordApp(b, count)
		for _, workload := range []struct {
			name string
			q    url.Values
		}{
			{name: "default_page", q: url.Values{"limit": {"100"}}},
			{name: "sorted_page", q: url.Values{"limit": {"100"}, "sort": {"amount"}, "order": {"desc"}}},
			{name: "aggregate", q: url.Values{"aggregate": {"count,amount:avg"}}},
			{name: "distinct", q: url.Values{"distinct": {"category"}}},
			{name: "grouped", q: url.Values{"group_by": {"category"}, "aggregate": {"count,amount:avg"}}},
		} {
			b.Run(fmt.Sprintf("rows_%d/%s", count, workload.name), func(b *testing.B) {
				runtime.GC()
				var baseline runtime.MemStats
				runtime.ReadMemStats(&baseline)
				peak := atomic.Uint64{}
				peak.Store(baseline.HeapAlloc)
				stop, done := make(chan struct{}), make(chan struct{})
				go func() {
					defer close(done)
					ticker := time.NewTicker(200 * time.Microsecond)
					defer ticker.Stop()
					for {
						select {
						case <-stop:
							return
						case <-ticker.C:
							var stats runtime.MemStats
							runtime.ReadMemStats(&stats)
							for prior := peak.Load(); stats.HeapAlloc > prior && !peak.CompareAndSwap(prior, stats.HeapAlloc); prior = peak.Load() {
							}
						}
					}
				}()
				b.ResetTimer()
				var result map[string]any
				for i := 0; i < b.N; i++ {
					var ae *apiError
					result, ae = a.queryRecords("bench", "events", workload.q)
					if ae != nil {
						b.Fatal(ae)
					}
				}
				runtime.KeepAlive(result)
				b.StopTimer()
				close(stop)
				<-done
				var final runtime.MemStats
				runtime.ReadMemStats(&final)
				for prior := peak.Load(); final.HeapAlloc > prior && !peak.CompareAndSwap(prior, final.HeapAlloc); prior = peak.Load() {
				}
				b.ReportMetric(float64(peak.Load()-baseline.HeapAlloc), "peak-heap-B")
			})
		}
	}
}
