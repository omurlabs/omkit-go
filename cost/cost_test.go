package cost

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func value(t *testing.T, service, provider, op, bucket string) float64 {
	t.Helper()
	return testutil.ToFloat64(CostUnitsTotal.WithLabelValues(service, provider, op, bucket))
}

func TestRecordCostIncrementsCounter(t *testing.T) {
	before := value(t, "cortex", "voyage", "embed", "paid")
	RecordCost("cortex", "voyage", "embed", 12, "paid")
	after := value(t, "cortex", "voyage", "embed", "paid")
	if after-before != 12 {
		t.Fatalf("delta = %v, want 12", after-before)
	}
}

func TestRecordCostNormalisesUnknownBucketToTrial(t *testing.T) {
	before := value(t, "cortex", "voyage", "embed", "trial")
	RecordCost("cortex", "voyage", "embed", 1, "enterprise")
	after := value(t, "cortex", "voyage", "embed", "trial")
	if after-before != 1 {
		t.Fatalf("trial delta = %v, want 1", after-before)
	}
}

func TestRecordCostSkipsZeroAndNegativeUnits(t *testing.T) {
	before := value(t, "synapse", "deepgram", "stt_seconds", "system")
	RecordCost("synapse", "deepgram", "stt_seconds", 0, "system")
	RecordCost("synapse", "deepgram", "stt_seconds", -5, "system")
	after := value(t, "synapse", "deepgram", "stt_seconds", "system")
	if after != before {
		t.Fatalf("counter changed on non-positive input: before=%v after=%v", before, after)
	}
}

func TestEachDocumentedBucketRecords(t *testing.T) {
	for _, bucket := range []string{"system", "trial", "paid"} {
		before := value(t, "spine", "cohere", "rerank", bucket)
		RecordCost("spine", "cohere", "rerank", 3, bucket)
		after := value(t, "spine", "cohere", "rerank", bucket)
		if after-before != 3 {
			t.Errorf("bucket %q delta = %v, want 3", bucket, after-before)
		}
	}
}
