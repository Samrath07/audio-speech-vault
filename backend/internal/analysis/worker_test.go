package analysis

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestRMSDBFS(t *testing.T) {
	pcm := make([]byte, 200)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], uint16(int16(16384)))
	}
	got := rmsDBFS(pcm)
	if math.Abs(got-(-6.02)) > 0.02 {
		t.Fatalf("rmsDBFS() = %v, want approximately -6.02", got)
	}
	if got := rmsDBFS(make([]byte, 200)); got != -100 {
		t.Fatalf("silent rmsDBFS() = %v, want -100", got)
	}
}

func TestDetectPausesAppliesMinimumAndLongThresholds(t *testing.T) {
	values := []float64{-10, -100, -100, -10, -100, -100, -100, -100, -10}
	points := make([]EnvelopePoint, len(values))
	for index, value := range values {
		points[index] = EnvelopePoint{TimeMS: int64(index * 100), DBFS: value}
	}
	events, silence := detectPauses(points, 900, defaultConfig())
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].StartMS != 400 || events[0].EndMS != 800 || events[0].Type != "pause" {
		t.Fatalf("unexpected pause: %+v", events[0])
	}
	if silence != 400 {
		t.Fatalf("silence = %d, want 400", silence)
	}

	longPoints := make([]EnvelopePoint, 25)
	for index := range longPoints {
		longPoints[index] = EnvelopePoint{TimeMS: int64(index * 100), DBFS: -100}
	}
	events, _ = detectPauses(longPoints, 2500, defaultConfig())
	if len(events) != 1 || events[0].Type != "long_pause" {
		t.Fatalf("expected one long pause, got %+v", events)
	}
}

func TestRhythmVectorIsFiniteForShortRecording(t *testing.T) {
	points := []EnvelopePoint{{TimeMS: 0, DBFS: -20}, {TimeMS: 100, DBFS: -30}}
	vector := rhythmVector(points, nil, 200)
	if len(vector) != 4 {
		t.Fatalf("vector length = %d, want 4", len(vector))
	}
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("non-finite rhythm value: %v", value)
		}
	}
}

func TestDetectRepetitionsRequiresAcousticAndDurationSimilarity(t *testing.T) {
	chunks := []SpeechChunk{
		{StartMS: 0, EndMS: 1000, Feature: []float64{-20, .08, -30, -28, -35, -42}},
		{StartMS: 1200, EndMS: 2250, Feature: []float64{-20.5, .081, -30.2, -28.1, -35.2, -41.8}},
		{StartMS: 2400, EndMS: 7000, Feature: []float64{-8, .3, -10, -12, -15, -18}},
	}
	events := detectRepetitions(chunks, defaultConfig())
	if len(events) != 1 || events[0].Type != "repetition_candidate" {
		t.Fatalf("expected one repetition candidate, got %+v", events)
	}
	if events[0].Score == nil || *events[0].Score < .72 {
		t.Fatalf("unexpected repetition score: %+v", events[0].Score)
	}
}

func TestSpeakerClusteringProducesAnonymousCandidates(t *testing.T) {
	chunks := []SpeechChunk{
		{StartMS: 0, EndMS: 500, Feature: []float64{-20, .05, -30, -28, -40, -50}},
		{StartMS: 700, EndMS: 1200, Feature: []float64{-8, .25, -12, -15, -18, -20}},
		{StartMS: 1400, EndMS: 1900, Feature: []float64{-21, .06, -31, -29, -39, -49}},
		{StartMS: 2100, EndMS: 2600, Feature: []float64{-9, .24, -13, -14, -19, -21}},
	}
	events := clusterSpeakers(chunks, defaultConfig())
	if len(events) < len(chunks) {
		t.Fatalf("expected speaker candidates, got %+v", events)
	}
	for _, chunk := range chunks {
		if chunk.Speaker != "speaker_a" && chunk.Speaker != "speaker_b" {
			t.Fatalf("chunk has no anonymous speaker assignment: %+v", chunk)
		}
	}
}

func TestConfigurablePauseThresholds(t *testing.T) {
	points := []EnvelopePoint{{TimeMS: 0, DBFS: -20}, {TimeMS: 100, DBFS: -40}, {TimeMS: 200, DBFS: -40}, {TimeMS: 300, DBFS: -20}}
	strict := defaultConfig()
	strict.MinimumPauseMS = 300
	if events, _ := detectPauses(points, 400, strict); len(events) != 0 {
		t.Fatalf("strict profile produced events: %+v", events)
	}
	permissive := strict
	permissive.MinimumPauseMS = 200
	if events, _ := detectPauses(points, 400, permissive); len(events) != 1 {
		t.Fatalf("permissive profile produced %d events, want 1", len(events))
	}
}

func TestVectorDistanceAndCSVGuardrail(t *testing.T) {
	if distance := vectorDistance([]float64{.2, 3, -20, 4}, []float64{.2, 3, -20, 4}); distance != 0 {
		t.Fatalf("identical vector distance = %v, want 0", distance)
	}
	for _, value := range []string{"=SUM(A1:A2)", "+1", "-1", "@cmd"} {
		if protected := csvSafe(value); protected[0] != '\'' {
			t.Fatalf("csvSafe(%q) = %q, want apostrophe prefix", value, protected)
		}
	}
}
