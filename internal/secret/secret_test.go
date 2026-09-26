package secret_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquemenezes/arca/internal/secret"
)

// ---------- wordlist ----------

func TestWordlistIsTheFullEFFLargeList(t *testing.T) {
	words := secret.Words()
	if len(words) != 7776 {
		t.Fatalf("wordlist has %d entries, want 7776 (6^5, the diceware size)", len(words))
	}
	seen := map[string]bool{}
	for _, w := range words {
		if seen[w] {
			t.Fatalf("duplicate word %q would skew the entropy calculation", w)
		}
		seen[w] = true
		if w == "" || strings.ContainsAny(w, " \t") {
			t.Fatalf("malformed word %q", w)
		}
	}
}

func TestEntropyMatchesWordlistSize(t *testing.T) {
	want := 6 * math.Log2(7776)
	if got := secret.EntropyBits(6); math.Abs(got-want) > 0.01 {
		t.Errorf("EntropyBits(6) = %.2f, want %.2f", got, want)
	}
	if got := secret.EntropyBits(secret.DefaultWords); got < 77 {
		t.Errorf("default passphrase carries only %.1f bits; design calls for ~77", got)
	}
}

// ---------- generation ----------

func TestGenerateProducesWordsFromTheList(t *testing.T) {
	in := map[string]bool{}
	for _, w := range secret.Words() {
		in[w] = true
	}

	got, err := secret.Generate(secret.DefaultWords)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	parts := strings.Split(got, secret.Separator)
	if len(parts) != secret.DefaultWords {
		t.Fatalf("got %d parts (%q), want %d", len(parts), got, secret.DefaultWords)
	}
	for _, p := range parts {
		if !in[p] {
			t.Errorf("word %q is not in the wordlist", p)
		}
	}
}

func TestGenerateIsRandom(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		p, err := secret.Generate(secret.DefaultWords)
		if err != nil {
			t.Fatal(err)
		}
		if seen[p] {
			t.Fatalf("Generate repeated a passphrase after %d draws", i)
		}
		seen[p] = true
	}
}

// A biased index would quietly shrink the keyspace, so check the draws spread
// across the list rather than clustering.
func TestGenerateDrawsAcrossTheWholeList(t *testing.T) {
	distinct := map[string]bool{}
	for i := 0; i < 500; i++ {
		p, err := secret.Generate(6)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range strings.Split(p, secret.Separator) {
			distinct[w] = true
		}
	}
	if len(distinct) < 1500 {
		t.Errorf("only %d distinct words across 3000 draws; selection looks biased", len(distinct))
	}
}

func TestGenerateRejectsTooFewWords(t *testing.T) {
	for _, n := range []int{-1, 0, 1, secret.MinWords - 1} {
		if _, err := secret.Generate(n); err == nil {
			t.Errorf("Generate(%d) should have failed", n)
		}
	}
	if _, err := secret.Generate(secret.MinWords); err != nil {
		t.Errorf("Generate(%d) should be allowed: %v", secret.MinWords, err)
	}
}

// ---------- strength ----------

func TestWeakPassphrasesAreRejected(t *testing.T) {
	for _, weak := range []string{
		"senha123", "password", "12345678", "aaaaaaaaaa",
		"qwerty", "backup2026", "", "abc",
	} {
		t.Run(weak, func(t *testing.T) {
			err := secret.CheckStrength(weak)
			if err == nil {
				t.Fatalf("%q was accepted", weak)
			}
			if !errors.Is(err, secret.ErrWeak) {
				t.Errorf("error should wrap ErrWeak, got %v", err)
			}
		})
	}
}

func TestGeneratedPassphraseIsAccepted(t *testing.T) {
	for i := 0; i < 20; i++ {
		p, err := secret.Generate(secret.DefaultWords)
		if err != nil {
			t.Fatal(err)
		}
		if err := secret.CheckStrength(p); err != nil {
			t.Fatalf("generated passphrase %q rejected: %v", p, err)
		}
	}
}

func TestStrongHumanPassphraseIsAccepted(t *testing.T) {
	if err := secret.CheckStrength("caravela-turquesa-9-relogio-abissal"); err != nil {
		t.Errorf("a strong human passphrase was rejected: %v", err)
	}
}

// A pathologically long input must not stall the tool.
func TestVeryLongPassphraseIsAcceptedQuickly(t *testing.T) {
	long := strings.Repeat("correct horse battery staple ", 200)
	done := make(chan error, 1)
	go func() { done <- secret.CheckStrength(long) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("long passphrase rejected: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CheckStrength stalled on a long input")
	}
}

func TestEstimateReportsAScore(t *testing.T) {
	weak := secret.Estimate("password")
	strong := secret.Estimate("caravela-turquesa-9-relogio-abissal")
	if weak.Score >= strong.Score {
		t.Errorf("scores not ordered: weak=%d strong=%d", weak.Score, strong.Score)
	}
	if weak.Summary == "" || strong.Summary == "" {
		t.Error("Summary should always carry a human label")
	}
}

// ---------- passphrase files ----------

func writeFile(t *testing.T, name, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFileTrimsTheTrailingNewline(t *testing.T) {
	p := writeFile(t, "pass", "correct-horse-battery-staple\n", 0o600)
	got, err := secret.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "correct-horse-battery-staple" {
		t.Errorf("got %q", got)
	}
}

// An editor leaving a CRLF must not silently become part of the passphrase.
func TestReadFileTrimsCRLF(t *testing.T) {
	p := writeFile(t, "pass", "correct-horse-battery-staple\r\n", 0o600)
	got, _ := secret.ReadFile(p)
	if string(got) != "correct-horse-battery-staple" {
		t.Errorf("got %q", got)
	}
}

func TestReadFileRefusesWorldReadable(t *testing.T) {
	p := writeFile(t, "pass", "correct-horse-battery-staple\n", 0o644)
	_, err := secret.ReadFile(p)
	if err == nil {
		t.Fatal("a passphrase file readable by others was accepted")
	}
	if !strings.Contains(err.Error(), "0644") {
		t.Errorf("error should name the mode, got: %v", err)
	}
}

func TestReadFileRejectsEmpty(t *testing.T) {
	p := writeFile(t, "pass", "\n", 0o600)
	if _, err := secret.ReadFile(p); err == nil {
		t.Error("an empty passphrase file was accepted")
	}
}

func TestReadFileRejectsMissing(t *testing.T) {
	if _, err := secret.ReadFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing passphrase file was accepted")
	}
}

// ---------- interactive prompt ----------

func TestPrompterRequiresMatchingConfirmation(t *testing.T) {
	var out bytes.Buffer
	replies := []string{"correct-horse-battery-staple", "typo-horse-battery-staple"}
	i := 0
	p := &secret.Prompter{
		Out: &out,
		ReadSecret: func(string) ([]byte, error) {
			r := replies[i]
			i++
			return []byte(r), nil
		},
	}

	if _, err := p.Passphrase(true); err == nil {
		t.Fatal("mismatched confirmation was accepted")
	}
}

func TestPrompterReturnsConfirmedPassphrase(t *testing.T) {
	var out bytes.Buffer
	p := &secret.Prompter{
		Out: &out,
		ReadSecret: func(string) ([]byte, error) {
			return []byte("correct-horse-battery-staple"), nil
		},
	}
	got, err := p.Passphrase(true)
	if err != nil {
		t.Fatalf("Passphrase: %v", err)
	}
	if string(got) != "correct-horse-battery-staple" {
		t.Errorf("got %q", got)
	}
}

func TestPrompterRejectsEmptyInput(t *testing.T) {
	var out bytes.Buffer
	p := &secret.Prompter{
		Out:        &out,
		ReadSecret: func(string) ([]byte, error) { return nil, nil },
	}
	if _, err := p.Passphrase(false); err == nil {
		t.Error("an empty passphrase was accepted")
	}
}

// ---------- hygiene ----------

func TestZeroClearsTheBuffer(t *testing.T) {
	b := []byte("correct-horse-battery-staple")
	secret.Zero(b)
	for i, c := range b {
		if c != 0 {
			t.Fatalf("byte %d not cleared: %q", i, c)
		}
	}
	secret.Zero(nil) // must not panic
}
