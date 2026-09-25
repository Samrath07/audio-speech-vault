package analysis

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	AlgorithmVersion = "signal-v3-p1"
	sampleRate       = 8000
	windowMS         = 100
	silenceDB        = -35.0
	minimumPauseMS   = 300
	longPauseMS      = 2000
)

type AnalysisConfig struct {
	SilenceDBFS          float64 `json:"silenceDbfs"`
	MinimumPauseMS       int64   `json:"minimumPauseMs"`
	LongPauseMS          int64   `json:"longPauseMs"`
	RepetitionSimilarity float64 `json:"repetitionSimilarity"`
	SpeakerSeparation    float64 `json:"speakerSeparation"`
}

func defaultConfig() AnalysisConfig {
	return AnalysisConfig{SilenceDBFS: silenceDB, MinimumPauseMS: minimumPauseMS, LongPauseMS: longPauseMS, RepetitionSimilarity: .72, SpeakerSeparation: .5}
}

type EnvelopePoint struct {
	TimeMS int64   `json:"timeMs"`
	DBFS   float64 `json:"dbfs"`
}

type Event struct {
	Type    string         `json:"type"`
	StartMS int64          `json:"startMs"`
	EndMS   int64          `json:"endMs"`
	Score   *float64       `json:"score,omitempty"`
	Payload map[string]any `json:"payload"`
}

type Result struct {
	RecordingID     string          `json:"recordingId"`
	Algorithm       string          `json:"algorithmVersion"`
	AudioSHA256     string          `json:"audioSha256"`
	DurationMS      int64           `json:"durationMs"`
	SampleRate      int             `json:"sampleRate"`
	Channels        int             `json:"channels"`
	PauseCount      int             `json:"pauseCount"`
	SilenceMS       int64           `json:"silenceMs"`
	ThresholdDBFS   float64         `json:"silenceThresholdDbfs"`
	MinimumPauseMS  int64           `json:"minimumPauseMs"`
	LongPauseMS     int64           `json:"longPauseMs"`
	Loudness        []EnvelopePoint `json:"loudnessEnvelope"`
	RhythmVector    []float64       `json:"rhythmVector"`
	SpeechChunks    []SpeechChunk   `json:"speechChunks"`
	PriorityScore   float64         `json:"priorityScore"`
	PriorityDetails map[string]any  `json:"priorityBreakdown"`
	Events          []Event         `json:"events"`
	GeneratedAt     time.Time       `json:"generatedAt"`
	FFmpegAvailable bool            `json:"ffmpegAvailable"`
	Configuration   AnalysisConfig  `json:"configuration"`
}

type job struct {
	ID            string
	RecordingID   string
	StorageKey    string
	Attempts      int
	MaxAttempts   int
	OriginalName  string
	Algorithm     string
	Configuration AnalysisConfig
}

type Worker struct {
	db      *pgxpool.Pool
	dataDir string
	logger  *slog.Logger
}

func NewWorker(db *pgxpool.Pool, dataDir string, logger *slog.Logger) *Worker {
	return &Worker{db: db, dataDir: dataDir, logger: logger}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := w.runNext(ctx); err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Error("analysis worker iteration failed", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) runNext(ctx context.Context) error {
	item, ok, err := w.claim(ctx)
	if err != nil || !ok {
		return err
	}
	w.logger.Info("signal analysis started", slog.String("recording_id", item.RecordingID), slog.Int("attempt", item.Attempts))
	result, err := analyze(ctx, item.RecordingID, filepath.Join(w.dataDir, "audio", item.StorageKey), item.Algorithm, item.Configuration)
	if err != nil {
		w.fail(ctx, item, err)
		return nil
	}
	metadataPath := filepath.Join("metadata", item.RecordingID, item.Algorithm+".json")
	reportPath := filepath.Join("reports", item.RecordingID, item.Algorithm+".txt")
	if err = writeJSONAtomic(filepath.Join(w.dataDir, metadataPath), result); err == nil {
		err = writeAtomic(filepath.Join(w.dataDir, reportPath), []byte(report(item.OriginalName, result)))
	}
	if err == nil {
		err = w.complete(ctx, item, result, filepath.ToSlash(metadataPath), filepath.ToSlash(reportPath))
	}
	if err != nil {
		w.fail(ctx, item, err)
		return nil
	}
	w.logger.Info("signal analysis completed", slog.String("recording_id", item.RecordingID), slog.Int("events", len(result.Events)))
	return nil
}

func (w *Worker) claim(ctx context.Context) (job, bool, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return job{}, false, err
	}
	defer tx.Rollback(ctx)
	_, _ = tx.Exec(ctx, `UPDATE analysis_jobs SET status='queued',available_at=NOW(),updated_at=NOW(),error_code='worker_timeout',error_message='Worker heartbeat expired' WHERE status='processing' AND heartbeat_at < NOW()-INTERVAL '5 minutes'`)
	var item job
	var configuration []byte
	err = tx.QueryRow(ctx, `SELECT j.id,j.recording_id,r.storage_key,j.attempts,j.max_attempts,r.original_filename,j.algorithm_version,j.configuration
		FROM analysis_jobs j JOIN recordings r ON r.id=j.recording_id
		WHERE j.status='queued' AND j.available_at<=NOW() AND j.attempts<j.max_attempts
		ORDER BY j.created_at FOR UPDATE OF j SKIP LOCKED LIMIT 1`).Scan(&item.ID, &item.RecordingID, &item.StorageKey, &item.Attempts, &item.MaxAttempts, &item.OriginalName, &item.Algorithm, &configuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return job{}, false, nil
	}
	if err != nil {
		return job{}, false, err
	}
	item.Attempts++
	item.Configuration = defaultConfig()
	_ = json.Unmarshal(configuration, &item.Configuration)
	_, err = tx.Exec(ctx, `UPDATE analysis_jobs SET status='processing',attempts=$2,started_at=NOW(),heartbeat_at=NOW(),error_code=NULL,error_message=NULL,updated_at=NOW() WHERE id=$1`, item.ID, item.Attempts)
	if err != nil {
		return job{}, false, err
	}
	return item, true, tx.Commit(ctx)
}

func analyze(ctx context.Context, recordingID, path, algorithm string, config AnalysisConfig) (Result, error) {
	result := Result{RecordingID: recordingID, Algorithm: algorithm, SampleRate: sampleRate, Channels: 1, ThresholdDBFS: config.SilenceDBFS, MinimumPauseMS: config.MinimumPauseMS, LongPauseMS: config.LongPauseMS, GeneratedAt: time.Now().UTC(), FFmpegAvailable: true, Loudness: []EnvelopePoint{}, Events: []Event{}, SpeechChunks: []SpeechChunk{}, Configuration: config}
	file, err := os.Open(path)
	if err != nil {
		return result, fmt.Errorf("open source audio: %w", err)
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		file.Close()
		return result, fmt.Errorf("checksum source audio: %w", err)
	}
	file.Close()
	result.AudioSHA256 = hex.EncodeToString(hash.Sum(nil))

	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-i", path, "-map_metadata", "-1", "-vn", "-ac", "1", "-ar", strconv.Itoa(sampleRate), "-f", "s16le", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("open decoder output: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{writer: &stderr, remaining: 4096}
	if err = cmd.Start(); err != nil {
		return result, fmt.Errorf("start ffmpeg: %w", err)
	}
	windowBytes := sampleRate * windowMS / 1000 * 2
	reader := bufio.NewReaderSize(stdout, windowBytes*2)
	buffer := make([]byte, windowBytes)
	var timeMS int64
	acoustic := []acousticPoint{}
	for {
		n, readErr := io.ReadFull(reader, buffer)
		if n > 1 {
			point := analyzeWindow(buffer[:n], timeMS)
			acoustic = append(acoustic, point)
			result.Loudness = append(result.Loudness, point.EnvelopePoint)
			timeMS += int64(n/2) * 1000 / sampleRate
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			return result, fmt.Errorf("read decoded audio: %w", readErr)
		}
	}
	if err = cmd.Wait(); err != nil {
		return result, fmt.Errorf("ffmpeg decode failed: %s", strings.TrimSpace(stderr.String()))
	}
	if len(result.Loudness) == 0 {
		return result, errors.New("decoded audio contains no samples")
	}
	result.DurationMS = timeMS
	result.Events, result.SilenceMS = detectPauses(result.Loudness, timeMS, config)
	result.PauseCount = len(result.Events)
	result.RhythmVector = rhythmVector(result.Loudness, result.Events, timeMS)
	result.SpeechChunks = buildSpeechChunks(acoustic, timeMS, config)
	result.Events = append(result.Events, detectRepetitions(result.SpeechChunks, config)...)
	result.Events = append(result.Events, clusterSpeakers(result.SpeechChunks, config)...)
	result.PriorityScore, result.PriorityDetails = priority(result)
	return result, nil
}

func rmsDBFS(pcm []byte) float64 {
	var sum float64
	count := len(pcm) / 2
	for i := 0; i < count; i++ {
		sample := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
		sum += sample * sample
	}
	if count == 0 || sum == 0 {
		return -100
	}
	return math.Round(20*math.Log10(math.Sqrt(sum/float64(count)))*100) / 100
}

func detectPauses(points []EnvelopePoint, durationMS int64, config AnalysisConfig) ([]Event, int64) {
	events := []Event{}
	var start int64 = -1
	var silenceMS int64
	flush := func(end int64) {
		if start < 0 || end-start < config.MinimumPauseMS {
			start = -1
			return
		}
		eventType := "pause"
		if end-start >= config.LongPauseMS {
			eventType = "long_pause"
		}
		events = append(events, Event{Type: eventType, StartMS: start, EndMS: end, Payload: map[string]any{"thresholdDbfs": config.SilenceDBFS}})
		silenceMS += end - start
		start = -1
	}
	for _, point := range points {
		if point.DBFS <= config.SilenceDBFS {
			if start < 0 {
				start = point.TimeMS
			}
		} else {
			flush(point.TimeMS)
		}
	}
	flush(durationMS)
	return events, silenceMS
}

func rhythmVector(points []EnvelopePoint, events []Event, durationMS int64) []float64 {
	var mean, squared float64
	for _, point := range points {
		mean += point.DBFS
	}
	mean /= float64(len(points))
	for _, point := range points {
		delta := point.DBFS - mean
		squared += delta * delta
	}
	stddev := math.Sqrt(squared / float64(len(points)))
	var silence int64
	for _, event := range events {
		silence += event.EndMS - event.StartMS
	}
	minutes := math.Max(float64(durationMS)/60000, 1.0/60)
	return []float64{round(float64(silence) / float64(durationMS)), round(float64(len(events)) / minutes), round(mean), round(stddev)}
}

func round(value float64) float64 { return math.Round(value*10000) / 10000 }

func (w *Worker) complete(ctx context.Context, item job, result Result, metadataPath, reportPath string) error {
	envelope, _ := json.Marshal(result.Loudness)
	rhythm, _ := json.Marshal(result.RhythmVector)
	chunks, _ := json.Marshal(result.SpeechChunks)
	priorityDetails, _ := json.Marshal(result.PriorityDetails)
	configuration, _ := json.Marshal(result.Configuration)
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var analysisID string
	err = tx.QueryRow(ctx, `INSERT INTO recording_analyses(recording_id,job_id,algorithm_version,audio_sha256,duration_ms,sample_rate,channels,pause_count,silence_ms,loudness_envelope,rhythm_vector,speech_chunks,priority_score,priority_breakdown,metadata_path,report_path,configuration)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT(recording_id,algorithm_version) DO UPDATE SET job_id=EXCLUDED.job_id,audio_sha256=EXCLUDED.audio_sha256,duration_ms=EXCLUDED.duration_ms,sample_rate=EXCLUDED.sample_rate,channels=EXCLUDED.channels,pause_count=EXCLUDED.pause_count,silence_ms=EXCLUDED.silence_ms,loudness_envelope=EXCLUDED.loudness_envelope,rhythm_vector=EXCLUDED.rhythm_vector,speech_chunks=EXCLUDED.speech_chunks,priority_score=EXCLUDED.priority_score,priority_breakdown=EXCLUDED.priority_breakdown,metadata_path=EXCLUDED.metadata_path,report_path=EXCLUDED.report_path,configuration=EXCLUDED.configuration,created_at=NOW()
		RETURNING id`, item.RecordingID, item.ID, result.Algorithm, result.AudioSHA256, result.DurationMS, result.SampleRate, result.Channels, result.PauseCount, result.SilenceMS, envelope, rhythm, chunks, result.PriorityScore, priorityDetails, metadataPath, reportPath, configuration).Scan(&analysisID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM signal_events WHERE recording_id=$1 AND analysis_id=$2`, item.RecordingID, analysisID); err != nil {
		return err
	}
	for _, event := range result.Events {
		payload, _ := json.Marshal(event.Payload)
		if _, err = tx.Exec(ctx, `INSERT INTO signal_events(analysis_id,recording_id,event_type,start_ms,end_ms,score,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, analysisID, item.RecordingID, event.Type, event.StartMS, event.EndMS, event.Score, payload); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE analysis_jobs SET status='completed',completed_at=NOW(),heartbeat_at=NOW(),updated_at=NOW() WHERE id=$1`, item.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *Worker) fail(ctx context.Context, item job, cause error) {
	message := cause.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	status := "failed"
	if item.Attempts < item.MaxAttempts {
		status = "queued"
	}
	_, err := w.db.Exec(ctx, `UPDATE analysis_jobs SET status=$2,available_at=NOW()+($3*INTERVAL '1 minute'),error_code='analysis_failed',error_message=$4,updated_at=NOW() WHERE id=$1`, item.ID, status, item.Attempts, message)
	if err != nil {
		w.logger.Error("failed to update analysis job", slog.Any("error", err))
	}
	w.logger.Error("signal analysis failed", slog.String("recording_id", item.RecordingID), slog.String("reason", message))
}

func writeJSONAtomic(path string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(contents, '\n'))
}

func writeAtomic(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".analysis-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(contents)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func report(name string, result Result) string {
	return fmt.Sprintf("Audio Speech Vault signal report\n\nRecording: %s\nSHA-256: %s\nAlgorithm: %s\nDuration: %d ms\nSample rate: %d Hz\nChannels analyzed: %d\nSilence threshold: %.1f dBFS\nMinimum pause: %d ms\nPause count: %d\nSilence duration: %d ms\nSpeech chunks: %d\nPriority score: %.2f\nPriority breakdown: %v\nRhythm vector: %v\nGenerated: %s\n\nThis report contains deterministic signal measurements. Repetition and speaker candidates require human review.\n", name, result.AudioSHA256, result.Algorithm, result.DurationMS, result.SampleRate, result.Channels, result.ThresholdDBFS, result.MinimumPauseMS, result.PauseCount, result.SilenceMS, len(result.SpeechChunks), result.PriorityScore, result.PriorityDetails, result.RhythmVector, result.GeneratedAt.Format(time.RFC3339))
}

type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *limitedWriter) Write(value []byte) (int, error) {
	original := len(value)
	if w.remaining > 0 {
		chunk := value
		if len(chunk) > w.remaining {
			chunk = chunk[:w.remaining]
		}
		_, _ = w.writer.Write(chunk)
		w.remaining -= len(chunk)
	}
	return original, nil
}
