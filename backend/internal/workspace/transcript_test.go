package workspace

import "testing"

func TestTranscriptFindingsRequireTargetAndContent(t *testing.T) {
	findings := transcriptFindings(Transcript{})
	want := map[string]bool{
		"target_speaker_unconfirmed": false,
		"transcript_empty":           false,
	}
	for _, finding := range findings {
		if _, ok := want[finding.Code]; ok && finding.Severity == "error" {
			want[finding.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("expected blocking finding %q, got %#v", code, findings)
		}
	}
}

func TestTranscriptFindingsAcceptCompleteTargetTranscript(t *testing.T) {
	source := "manual"
	findings := transcriptFindings(Transcript{
		TargetSpeakerSource: &source,
		Segments:            []TranscriptSegment{{SpeakerRole: "target", Text: "I... I went, uh, home."}},
	})
	for _, finding := range findings {
		if finding.Severity == "error" {
			t.Fatalf("complete transcript returned blocking finding: %#v", finding)
		}
	}
}

func TestTranscriptFindingsWarnForUnknownSpeaker(t *testing.T) {
	source := "speaker_a"
	findings := transcriptFindings(Transcript{
		TargetSpeakerSource: &source,
		Segments: []TranscriptSegment{
			{SpeakerRole: "target", Text: "yes"},
			{SpeakerRole: "unknown", Text: "[unintelligible]"},
		},
	})
	if len(findings) != 1 || findings[0].Code != "unknown_speaker" || findings[0].Severity != "warning" {
		t.Fatalf("expected one unknown-speaker warning, got %#v", findings)
	}
}

func TestValidSpeakerRole(t *testing.T) {
	for _, role := range []string{"target", "interviewer", "other", "unknown", "overlap"} {
		if !validSpeakerRole(role) {
			t.Fatalf("expected %q to be valid", role)
		}
	}
	for _, role := range []string{"", "speaker_a", "admin", "TARGET"} {
		if validSpeakerRole(role) {
			t.Fatalf("expected %q to be rejected", role)
		}
	}
}
