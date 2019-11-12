package metric

import (
	"fmt"
	"io/ioutil"
	"path"
	"testing"
	"time"

	"github.com/newrelic/infra-integrations-sdk/data/attribute"
	"github.com/newrelic/infra-integrations-sdk/log"
	"github.com/newrelic/infra-integrations-sdk/persist"
	"github.com/stretchr/testify/assert"
)

// growing time for tests to avoid errors generated by store avoiding samples too close in time
var (
	now         = time.Now()
	growingTime = func() time.Time {
		now = now.Add(1 * time.Second)
		return now
	}
)

var rateAndDeltaTests = []struct {
	testCase string
	key      string
	value    interface{}
	out      interface{}
	cache    interface{}
}{
	{"1st data in key", "key1", .22323333, 0.0, 0.22323333},
	{"1st data in key", "key2", 100, 0.0, 100.0},
	{"2nd data in key", "key2", 110, 10.0, 110.0},
}

func TestSet_SetMetricGauge(t *testing.T) {
	persist.SetNow(growingTime)

	ms := NewSet("some-event-type", nil)

	assert.NoError(t, ms.SetMetric("key", 10, GAUGE))

	assert.Equal(t, 10.0, ms.Metrics["key"], "stored gauge should be float")
}

func TestSet_SetMetricAttribute(t *testing.T) {
	persist.SetNow(growingTime)

	ms := NewSet("some-event-type", nil)

	ms.SetMetric("key", "some-attribute", ATTRIBUTE)

	if ms.Metrics["key"] != "some-attribute" {
		t.Errorf("metric stored not valid: %v", ms.Metrics["key"])
	}
}

func TestSet_SetMetricCachesRateAndDeltas(t *testing.T) {
	storer := persist.NewInMemoryStore()

	for _, sourceType := range []SourceType{DELTA, RATE, PRATE, PDELTA} {
		persist.SetNow(growingTime)

		ms := NewSet("some-event-type", storer, attribute.Attr("k", "v"))

		for _, tt := range rateAndDeltaTests {
			// user should not store different types under the same key
			key := fmt.Sprintf("%s:%d", tt.key, sourceType)
			ms.SetMetric(key, tt.value, sourceType)

			if ms.Metrics[key] != tt.out {
				t.Errorf("setting %s %s source-type %d and value %v returned: %v, expected: %v",
					tt.testCase, tt.key, sourceType, tt.value, ms.Metrics[key], tt.out)
			}

			var v interface{}
			_, err := storer.Get(ms.namespace(key), &v)
			if err == persist.ErrNotFound {
				t.Errorf("key %s not in cache for case %s", tt.key, tt.testCase)
			} else if tt.cache != v {
				t.Errorf("cache.Get(\"%v\") ==> %v, want %v", tt.key, v, tt.cache)
			}
		}
	}
}

func TestSet_SetMetricsRatesAndDeltas(t *testing.T) {
	var testCases = []struct {
		sourceType  SourceType
		firstValue  float64
		secondValue float64
		finalValue  float64
	}{
		{DELTA, 5, 2, -3.0},
		{DELTA, 2, 5, 3.0},
		{PDELTA, 1, 5, 4.0},
		{RATE, 2, 4, 2.0},
		{RATE, 4, 2, -2.0},
		{PRATE, 2, 4, 2.0},
	}

	for _, tc := range testCases {
		t.Run(string(tc.sourceType), func(t *testing.T) {

			persist.SetNow(growingTime)

			ms := NewSet("some-event-type", persist.NewInMemoryStore(), attribute.Attr("k", "v"))

			assert.NoError(t, ms.SetMetric("d", tc.firstValue, tc.sourceType))
			assert.NoError(t, ms.SetMetric("d", tc.secondValue, tc.sourceType))
			assert.Equal(t, ms.Metrics["d"], tc.finalValue)
		})
	}
}

func TestSet_SetMetricPositivesThrowsOnNegativeValues(t *testing.T) {
	for _, sourceType := range []SourceType{PDELTA, PRATE} {
		t.Run(string(sourceType), func(t *testing.T) {
			persist.SetNow(growingTime)
			ms := NewSet(
				"some-event-type",
				persist.NewInMemoryStore(),
				attribute.Attr("k", "v"),
			)
			assert.NoError(t, ms.SetMetric("d", 5, sourceType))
			assert.Error(
				t,
				ms.SetMetric("d", 2, sourceType),
				"source was reset, skipping",
			)
			assert.Equal(t, ms.Metrics["d"], 0.0)
		})
	}
}

func TestSet_SetMetric_NilStorer(t *testing.T) {
	ms := NewSet("some-event-type", nil)

	err := ms.SetMetric("foo", 1, RATE)
	assert.Error(t, err, "integrations built with no-store can't use DELTAs and RATEs")

	err = ms.SetMetric("foo", 1, DELTA)
	assert.Error(t, err, "integrations built with no-store can't use DELTAs and RATEs")
}

func TestSet_SetMetric_IncorrectMetricType(t *testing.T) {
	ms := NewSet("some-event-type", persist.NewInMemoryStore())

	err := ms.SetMetric("foo", "bar", RATE)
	assert.Error(t, err, "non-numeric source type for rate/delta metric foo")

	err = ms.SetMetric("foo", "bar", DELTA)
	assert.Error(t, err, "non-numeric source type for rate/delta metric foo")

	err = ms.SetMetric("foo", "bar", GAUGE)
	assert.Error(t, err, "non-numeric source type for gauge metric foo")

	err = ms.SetMetric("foo", 1, ATTRIBUTE)
	assert.Error(t, err, "non-string source type for attribute foo")

	err = ms.SetMetric("foo", 1, 666)
	assert.Error(t, err, "unknown source type for key foo")

}

func TestSet_MarshalJSON(t *testing.T) {
	ms := NewSet("some-event-type", persist.NewInMemoryStore(), attribute.Attr("k", "v"))

	ms.SetMetric("foo", 1, RATE)
	ms.SetMetric("bar", 1, DELTA)
	ms.SetMetric("baz", 1, GAUGE)
	ms.SetMetric("quux", "bar", ATTRIBUTE)

	marshaled, err := ms.MarshalJSON()

	assert.NoError(t, err)
	assert.Equal(t,
		`{"bar":0,"baz":1,"event_type":"some-event-type","foo":0,"k":"v","quux":"bar"}`,
		string(marshaled),
	)
}

func TestSet_UnmarshalJSON(t *testing.T) {
	ms := NewSet("some-event-type", persist.NewInMemoryStore(), attribute.Attr("k", "v"))

	err := ms.UnmarshalJSON([]byte(`{"foo":0,"bar":1.5,"quux":"bar"}`))

	assert.NoError(t, err)
	assert.Equal(t, 0., ms.Metrics["foo"])
	assert.Equal(t, 1.5, ms.Metrics["bar"])
	assert.Equal(t, "bar", ms.Metrics["quux"])
}

func TestNewSet_FileStore_StoresBetweenRuns(t *testing.T) {
	persist.SetNow(growingTime)

	storeFile := tempFile()

	s, err := persist.NewFileStore(storeFile, log.Discard, 1*time.Hour)
	assert.NoError(t, err)

	set1 := NewSet("type", s, attribute.Attr("k", "v"))

	assert.NoError(t, set1.SetMetric("foo", 1, DELTA))

	assert.NoError(t, s.Save())

	s2, err := persist.NewFileStore(storeFile, log.Discard, 1*time.Hour)
	assert.NoError(t, err)

	set2 := NewSet("type", s2, attribute.Attr("k", "v"))

	assert.NoError(t, set2.SetMetric("foo", 3, DELTA))

	assert.Equal(t, 2.0, set2.Metrics["foo"])
}

func TestNewSet_Attr_AddsAttributes(t *testing.T) {
	persist.SetNow(growingTime)

	storeFile := tempFile()

	// write in same store/integration-run
	storeWrite, err := persist.NewFileStore(storeFile, log.Discard, 1*time.Hour)
	assert.NoError(t, err)

	set := NewSet(
		"type",
		storeWrite,
		attribute.Attr("pod", "pod-a"),
		attribute.Attr("node", "node-a"),
	)

	assert.Equal(t, "pod-a", set.Metrics["pod"])
	assert.Equal(t, "node-a", set.Metrics["node"])
}

func TestNewSet_Attr_SolvesCacheCollision(t *testing.T) {
	persist.SetNow(growingTime)

	storeFile := tempFile()

	// write in same store/integration-run
	storeWrite, err := persist.NewFileStore(storeFile, log.Discard, 1*time.Hour)
	assert.NoError(t, err)

	ms1 := NewSet("type", storeWrite, attribute.Attr("pod", "pod-a"))
	ms2 := NewSet("type", storeWrite, attribute.Attr("pod", "pod-a"))
	ms3 := NewSet("type", storeWrite, attribute.Attr("pod", "pod-b"))

	assert.NoError(t, ms1.SetMetric("field", 1, DELTA))
	assert.NoError(t, ms2.SetMetric("field", 2, DELTA))
	assert.NoError(t, ms3.SetMetric("field", 3, DELTA))

	assert.NoError(t, storeWrite.Save())

	// retrieve from another store/integration-run
	storeRead, err := persist.NewFileStore(storeFile, log.Discard, 1*time.Hour)
	assert.NoError(t, err)

	msRead := NewSet("type", storeRead, attribute.Attr("pod", "pod-a"))

	// write is required to make data available for read
	assert.NoError(t, msRead.SetMetric("field", 10, DELTA))

	assert.Equal(t, 8.0, msRead.Metrics["field"], "read metric-set: %+v", msRead.Metrics)
}

func TestSet_namespace(t *testing.T) {
	s := NewSet("type", persist.NewInMemoryStore(), attribute.Attr("k", "v"))

	assert.Equal(t, fmt.Sprintf("k==v::foo"), s.namespace("foo"))

	// several attributed are supported
	s = NewSet(
		"type",
		persist.NewInMemoryStore(),
		attribute.Attr("k1", "v1"),
		attribute.Attr("k2", "v2"),
	)

	assert.Equal(t, fmt.Sprintf("k1==v1::k2==v2::foo"), s.namespace("foo"))

	// provided attributes order does not matter
	s = NewSet(
		"type",
		persist.NewInMemoryStore(),
		attribute.Attr("k2", "v2"),
		attribute.Attr("k1", "v1"),
	)

	assert.Equal(t, fmt.Sprintf("k1==v1::k2==v2::foo"), s.namespace("foo"))
}

func Test_castToFloat(t *testing.T) {
	testCases := []struct {
		input  interface{}
		output float64
		error  bool
	}{
		{true, 1., false},
		{false, 0, false},
		{1, 1., false},
		{2, 2., false},
		{1.5, 1.5, false},
		{"true", 0, true},
		{"false", 0, true},
	}

	for _, tc := range testCases {
		r, err := castToFloat(tc.input)

		if tc.error {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tc.output, r)
		}
	}
}

func tempFile() string {
	dir, err := ioutil.TempDir("", "file_store")
	if err != nil {
		panic(err)
	}

	return path.Join(dir, "test.json")
}
