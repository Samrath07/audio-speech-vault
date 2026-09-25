package analysis

import (
	"encoding/binary"
	"math"
)

type acousticPoint struct {
	EnvelopePoint
	ZCR   float64
	Bands [4]float64
}

type SpeechChunk struct {
	StartMS int64     `json:"startMs"`
	EndMS   int64     `json:"endMs"`
	Feature []float64 `json:"feature"`
	Speaker string    `json:"speaker,omitempty"`
}

func analyzeWindow(pcm []byte, timeMS int64) acousticPoint {
	count := len(pcm) / 2
	samples := make([]float64, count)
	zeroCrossings := 0
	for i := 0; i < count; i++ {
		samples[i] = float64(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
		if i > 0 && (samples[i] >= 0) != (samples[i-1] >= 0) {
			zeroCrossings++
		}
	}
	point := acousticPoint{EnvelopePoint: EnvelopePoint{TimeMS: timeMS, DBFS: rmsDBFS(pcm)}}
	if count > 1 {
		point.ZCR = float64(zeroCrossings) / float64(count-1)
	}
	frequencies := [4]float64{200, 500, 1000, 2000}
	for index, frequency := range frequencies {
		point.Bands[index] = goertzelDB(samples, frequency, sampleRate)
	}
	return point
}

func goertzelDB(samples []float64, frequency float64, rate int) float64 {
	omega := 2 * math.Pi * frequency / float64(rate)
	coefficient := 2 * math.Cos(omega)
	var previous, previous2 float64
	for _, sample := range samples {
		current := sample + coefficient*previous - previous2
		previous2, previous = previous, current
	}
	power := previous2*previous2 + previous*previous - coefficient*previous*previous2
	if power <= 1e-12 {
		return -120
	}
	return 10 * math.Log10(power/float64(len(samples)*len(samples)))
}

func buildSpeechChunks(points []acousticPoint, durationMS int64, config AnalysisConfig) []SpeechChunk {
	chunks := []SpeechChunk{}
	start := -1
	lastSpeech := -1
	for index, point := range points {
		if point.DBFS > config.SilenceDBFS {
			if start < 0 {
				start = index
			}
			lastSpeech = index
			continue
		}
		if start >= 0 && int64(index-lastSpeech)*windowMS >= config.MinimumPauseMS {
			chunks = appendChunk(chunks, points, start, lastSpeech+1)
			start, lastSpeech = -1, -1
		}
	}
	if start >= 0 {
		chunks = appendChunk(chunks, points, start, lastSpeech+1)
	}
	for index := range chunks {
		if chunks[index].EndMS > durationMS {
			chunks[index].EndMS = durationMS
		}
	}
	return chunks
}

func appendChunk(chunks []SpeechChunk, points []acousticPoint, start, end int) []SpeechChunk {
	if end <= start || int64(end-start)*windowMS < 200 {
		return chunks
	}
	feature := make([]float64, 6)
	for _, point := range points[start:end] {
		feature[0] += point.DBFS
		feature[1] += point.ZCR
		for band := range point.Bands {
			feature[band+2] += point.Bands[band]
		}
	}
	for index := range feature {
		feature[index] = round(feature[index] / float64(end-start))
	}
	return append(chunks, SpeechChunk{StartMS: points[start].TimeMS, EndMS: points[end-1].TimeMS + windowMS, Feature: feature})
}

func detectRepetitions(chunks []SpeechChunk, config AnalysisConfig) []Event {
	events := []Event{}
	for index := 1; index < len(chunks); index++ {
		left, right := chunks[index-1], chunks[index]
		leftDuration, rightDuration := float64(left.EndMS-left.StartMS), float64(right.EndMS-right.StartMS)
		durationRatio := math.Min(leftDuration, rightDuration) / math.Max(leftDuration, rightDuration)
		if durationRatio < 0.55 || right.StartMS-left.EndMS > 2500 {
			continue
		}
		distance := scaledDistance(left.Feature, right.Feature)
		similarity := math.Exp(-distance) * durationRatio
		if similarity < config.RepetitionSimilarity {
			continue
		}
		score := round(similarity)
		events = append(events, Event{Type: "repetition_candidate", StartMS: left.StartMS, EndMS: right.EndMS, Score: &score, Payload: map[string]any{"leftStartMs": left.StartMS, "leftEndMs": left.EndMS, "rightStartMs": right.StartMS, "rightEndMs": right.EndMS, "durationRatio": round(durationRatio), "featureDistance": round(distance)}})
	}
	return events
}

func scaledDistance(left, right []float64) float64 {
	scales := []float64{15, .12, 18, 18, 18, 18}
	var sum float64
	for index := range left {
		delta := (left[index] - right[index]) / scales[index]
		sum += delta * delta
	}
	return math.Sqrt(sum / float64(len(left)))
}

func clusterSpeakers(chunks []SpeechChunk, config AnalysisConfig) []Event {
	if len(chunks) < 4 {
		return nil
	}
	vectors := standardizedFeatures(chunks)
	centroids := [2][]float64{append([]float64(nil), vectors[0]...), nil}
	farthest := 1
	for index := 2; index < len(vectors); index++ {
		if euclidean(vectors[index], centroids[0]) > euclidean(vectors[farthest], centroids[0]) {
			farthest = index
		}
	}
	centroids[1] = append([]float64(nil), vectors[farthest]...)
	assignments := make([]int, len(chunks))
	for iteration := 0; iteration < 12; iteration++ {
		for index, vector := range vectors {
			if euclidean(vector, centroids[1]) < euclidean(vector, centroids[0]) {
				assignments[index] = 1
			} else {
				assignments[index] = 0
			}
		}
		centroids = recomputeCentroids(vectors, assignments, centroids)
	}
	separation := euclidean(centroids[0], centroids[1])
	if separation < config.SpeakerSeparation {
		return nil
	}
	events := []Event{}
	for index := range chunks {
		label := "speaker_a"
		if assignments[index] == 1 {
			label = "speaker_b"
		}
		chunks[index].Speaker = label
		distance := euclidean(vectors[index], centroids[assignments[index]])
		confidence := round(1 / (1 + distance))
		events = append(events, Event{Type: label, StartMS: chunks[index].StartMS, EndMS: chunks[index].EndMS, Score: &confidence, Payload: map[string]any{"clusterSeparation": round(separation), "anonymousCluster": true}})
		if index > 0 && assignments[index] != assignments[index-1] {
			events = append(events, Event{Type: "speaker_turn", StartMS: chunks[index].StartMS, EndMS: min64(chunks[index].StartMS+500, chunks[index].EndMS), Score: &confidence, Payload: map[string]any{"from": chunks[index-1].Speaker, "to": label}})
		}
	}
	return events
}

func standardizedFeatures(chunks []SpeechChunk) [][]float64 {
	mean := make([]float64, len(chunks[0].Feature))
	stddev := make([]float64, len(mean))
	for _, chunk := range chunks {
		for index, value := range chunk.Feature {
			mean[index] += value
		}
	}
	for index := range mean {
		mean[index] /= float64(len(chunks))
	}
	for _, chunk := range chunks {
		for index, value := range chunk.Feature {
			delta := value - mean[index]
			stddev[index] += delta * delta
		}
	}
	for index := range stddev {
		stddev[index] = math.Sqrt(stddev[index] / float64(len(chunks)))
		if stddev[index] < 1e-6 {
			stddev[index] = 1
		}
	}
	result := make([][]float64, len(chunks))
	for row, chunk := range chunks {
		result[row] = make([]float64, len(mean))
		for column, value := range chunk.Feature {
			result[row][column] = (value - mean[column]) / stddev[column]
		}
	}
	return result
}

func recomputeCentroids(vectors [][]float64, assignments []int, fallback [2][]float64) [2][]float64 {
	result := [2][]float64{make([]float64, len(vectors[0])), make([]float64, len(vectors[0]))}
	counts := [2]int{}
	for index, vector := range vectors {
		cluster := assignments[index]
		counts[cluster]++
		for column, value := range vector {
			result[cluster][column] += value
		}
	}
	for cluster := 0; cluster < 2; cluster++ {
		if counts[cluster] == 0 {
			result[cluster] = fallback[cluster]
			continue
		}
		for column := range result[cluster] {
			result[cluster][column] /= float64(counts[cluster])
		}
	}
	return result
}

func euclidean(left, right []float64) float64 {
	var sum float64
	for i := range left {
		delta := left[i] - right[i]
		sum += delta * delta
	}
	return math.Sqrt(sum)
}

func priority(result Result) (float64, map[string]any) {
	longPauses, repetitions, turns := 0, 0, 0
	for _, event := range result.Events {
		switch event.Type {
		case "long_pause":
			longPauses++
		case "repetition_candidate":
			repetitions++
		case "speaker_turn":
			turns++
		}
	}
	durationMinutes := float64(result.DurationMS) / 60000
	longPausePoints := math.Min(30, float64(longPauses)*1.5)
	repetitionPoints := math.Min(35, float64(repetitions)*5)
	turnPoints := math.Min(20, float64(turns)*.5)
	durationPoints := math.Min(15, durationMinutes)
	score := round(math.Min(100, longPausePoints+repetitionPoints+turnPoints+durationPoints))
	return score, map[string]any{"longPausePoints": round(longPausePoints), "repetitionPoints": round(repetitionPoints), "speakerTurnPoints": round(turnPoints), "durationPoints": round(durationPoints), "longPauseCount": longPauses, "repetitionCandidateCount": repetitions, "speakerTurnCount": turns}
}

func min64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
