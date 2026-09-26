// Package secret handles passphrases: generating strong ones, refusing weak
// ones, reading them without leaking, and clearing them afterwards.
//
// The passphrase is the weakest link in the whole design. age's scrypt gives
// margin, but nothing saves a guessable passphrase, so the default path here is
// to generate one rather than to accept one.
package secret

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"strings"
	"sync"

	"crypto/rand"
	"crypto/subtle"

	"github.com/trustelem/zxcvbn"
)

// wordlistData is "EFF's Long Wordlist", Copyright (c) 2016 Electronic
// Frontier Foundation, licensed CC BY 3.0 US. See the NOTICE file at the root
// of this repository for the full attribution.
//
//go:embed eff_large_wordlist.txt
var wordlistData string

const (
	// DefaultWords gives about 77 bits of entropy, comfortably beyond what an
	// offline attacker can grind through scrypt.
	DefaultWords = 6
	// MinWords is the floor for a generated passphrase (~52 bits).
	MinWords = 4
	// Separator joins the words. Note that the EFF list itself contains four
	// hyphenated entries (yo-yo, t-shirt, drop-down, felt-tip), so word
	// boundaries are not recoverable from the string. That costs nothing:
	// entropy comes from the draws, not from the punctuation.
	Separator = "-"
	// MinScore is the zxcvbn score (0-4) a user-supplied passphrase must reach.
	MinScore = 3
	// longEnough is the length above which a passphrase is accepted without
	// running the estimator, which keeps pathological inputs cheap.
	longEnough = 64
)

var (
	wordsOnce sync.Once
	words     []string
)

// Words returns the embedded EFF large diceware wordlist.
func Words() []string {
	wordsOnce.Do(func() {
		for _, line := range strings.Split(wordlistData, "\n") {
			if w := strings.TrimSpace(line); w != "" {
				words = append(words, w)
			}
		}
	})
	return words
}

// EntropyBits reports the entropy of an n-word generated passphrase.
func EntropyBits(n int) float64 {
	if n <= 0 {
		return 0
	}
	return float64(n) * math.Log2(float64(len(Words())))
}

// Generate draws a diceware passphrase from crypto/rand.
func Generate(n int) (string, error) {
	if n < MinWords {
		return "", fmt.Errorf("a generated passphrase needs at least %d words, got %d", MinWords, n)
	}
	list := Words()
	size := big.NewInt(int64(len(list)))

	parts := make([]string, n)
	for i := range parts {
		// rand.Int is uniform over [0, size): no modulo bias.
		idx, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", fmt.Errorf("reading randomness: %w", err)
		}
		parts[i] = list[idx.Int64()]
	}
	return strings.Join(parts, Separator), nil
}

// ---------- strength ----------

// ErrWeak marks a passphrase the tool refuses to use.
var ErrWeak = errors.New("passphrase is too weak")

// Strength is a human-facing estimate of how hard a passphrase is to guess.
type Strength struct {
	Score   int // zxcvbn, 0 (trivial) to 4 (strong)
	Guesses float64
	Summary string
}

var scoreLabels = [...]string{
	"trivially guessable",
	"very weak",
	"weak",
	"reasonable",
	"strong",
}

// Estimate scores a passphrase. Inputs longer than longEnough are accepted
// outright: at that length the estimator adds cost without adding judgement.
func Estimate(pass string) Strength {
	if len(pass) >= longEnough {
		return Strength{Score: 4, Guesses: math.Inf(1), Summary: scoreLabels[4]}
	}
	r := zxcvbn.PasswordStrength(pass, nil)
	score := r.Score
	if score < 0 {
		score = 0
	}
	if score > 4 {
		score = 4
	}
	return Strength{Score: score, Guesses: r.Guesses, Summary: scoreLabels[score]}
}

// CheckStrength enforces the minimum. The caller may offer an explicit
// override flag, but never silently.
func CheckStrength(pass string) error {
	if strings.TrimSpace(pass) == "" {
		return fmt.Errorf("%w: it is empty", ErrWeak)
	}
	s := Estimate(pass)
	if s.Score < MinScore {
		return fmt.Errorf("%w: rated %q (%d/4). Run `arca gen-passphrase` for a strong one, "+
			"or pass --allow-weak-passphrase if you accept the risk", ErrWeak, s.Summary, s.Score)
	}
	return nil
}

// ---------- reading ----------

// ReadFile loads a passphrase from a file, for non-interactive runs.
//
// This exists so the passphrase never has to travel through an environment
// variable, which would expose it in /proc/<pid>/environ and to anyone who can
// list processes.
func ReadFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("passphrase file: %w", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf(
			"passphrase file %s has mode %#o: it is readable by other users on this machine. "+
				"Fix it with: chmod 600 %s", path, mode, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("passphrase file: %w", err)
	}
	// Trim only trailing line endings: leading and inner spaces may be
	// deliberate parts of the passphrase.
	pass := strings.TrimRight(string(data), "\r\n")
	if strings.TrimSpace(pass) == "" {
		return nil, fmt.Errorf("passphrase file %s is empty", path)
	}
	return []byte(pass), nil
}

// Prompter reads a passphrase interactively. ReadSecret is injected so the
// terminal handling stays at the edge and the logic here remains testable.
type Prompter struct {
	Out        io.Writer
	ReadSecret func(prompt string) ([]byte, error)
}

// Passphrase asks once, or twice when confirm is set. A typo in a backup
// passphrase is unrecoverable, which is why writing asks for confirmation.
func (p *Prompter) Passphrase(confirm bool) ([]byte, error) {
	first, err := p.ReadSecret("Passphrase: ")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(first))) == 0 {
		Zero(first)
		return nil, errors.New("empty passphrase")
	}
	if !confirm {
		return first, nil
	}

	second, err := p.ReadSecret("Confirm passphrase: ")
	if err != nil {
		Zero(first)
		return nil, err
	}
	defer Zero(second)

	if subtle.ConstantTimeCompare(first, second) != 1 {
		Zero(first)
		return nil, errors.New("passphrases do not match")
	}
	return first, nil
}

// Zero clears a secret buffer.
//
// Best effort, and worth being honest about: Go's garbage collector may have
// already copied the bytes elsewhere, and nothing here prevents the passphrase
// reaching swap. Locking memory is out of scope.
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
