package codec_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/hamsa/arca/internal/codec"
)

// ---------- helpers ----------

func newIdentity(t *testing.T) (identity, recipient string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	return id.String(), id.Recipient().String()
}

func writeIdentityFile(t *testing.T, dir, name, identity string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(identity+"\n"), mode); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	// WriteFile is subject to umask; force the mode we asked for.
	if err := os.Chmod(p, mode); err != nil {
		t.Fatalf("chmod identity: %v", err)
	}
	return p
}

func seal(t *testing.T, c codec.Cipher, k *codec.Keyring, plaintext []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := c.Seal(&buf, k)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

func open(t *testing.T, c codec.Cipher, k *codec.Keyring, ciphertext []byte) ([]byte, error) {
	t.Helper()
	r, err := c.Open(bytes.NewReader(ciphertext), k)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// ---------- registry ----------

func TestRegistryKnowsBuiltins(t *testing.T) {
	if _, err := codec.GetCompressor("zstd"); err != nil {
		t.Errorf("zstd compressor not registered: %v", err)
	}
	if _, err := codec.GetCipher("age"); err != nil {
		t.Errorf("age cipher not registered: %v", err)
	}
}

func TestRegistryUnknownNameErrors(t *testing.T) {
	if _, err := codec.GetCompressor("brotli"); err == nil {
		t.Error("expected error for unknown compressor")
	}
	if _, err := codec.GetCipher("rot13"); err == nil {
		t.Error("expected error for unknown cipher")
	}
}

// ---------- zstd ----------

func TestZstdRoundTripAllLevels(t *testing.T) {
	c, err := codec.GetCompressor("zstd")
	if err != nil {
		t.Fatal(err)
	}
	// Compressible payload so we can also assert compression actually happens.
	plain := bytes.Repeat([]byte("arca backup payload 0123456789\n"), 4096)

	for _, lvl := range []codec.Level{codec.LevelFastest, codec.LevelDefault, codec.LevelBetter, codec.LevelBest} {
		t.Run(lvl.String(), func(t *testing.T) {
			var buf bytes.Buffer
			w, err := c.NewWriter(&buf, codec.CompressOpts{Level: lvl, Threads: 2})
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
			if _, err := w.Write(plain); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if buf.Len() >= len(plain) {
				t.Errorf("no compression: %d >= %d", buf.Len(), len(plain))
			}

			r, err := c.NewReader(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatalf("reader close: %v", err)
			}
			if !bytes.Equal(got, plain) {
				t.Error("round trip mismatch")
			}
		})
	}
}

func TestZstdWriterEmitsMagic(t *testing.T) {
	c, _ := codec.GetCompressor("zstd")
	var buf bytes.Buffer
	w, _ := c.NewWriter(&buf, codec.CompressOpts{Level: codec.LevelBest})
	w.Write([]byte("x"))
	w.Close()

	if !bytes.HasPrefix(buf.Bytes(), c.Magic()) {
		t.Errorf("output does not start with zstd magic: % x", buf.Bytes()[:4])
	}
	// The canonical zstd frame magic 0xFD2FB528 stored little-endian.
	if want := []byte{0x28, 0xB5, 0x2F, 0xFD}; !bytes.Equal(c.Magic(), want) {
		t.Errorf("Magic() = % x, want % x", c.Magic(), want)
	}
}

func TestLevelParsing(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want codec.Level
	}{
		{"fastest", codec.LevelFastest},
		{"default", codec.LevelDefault},
		{"better", codec.LevelBetter},
		{"best", codec.LevelBest},
		{"BEST", codec.LevelBest},
	} {
		got, err := codec.ParseLevel(tc.in)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := codec.ParseLevel("ludicrous"); err == nil {
		t.Error("expected error for unknown level")
	}
}

// ---------- age: passphrase mode ----------

func TestAgePassphraseRoundTrip(t *testing.T) {
	c, _ := codec.GetCipher("age")
	k := &codec.Keyring{Passphrase: []byte("correct horse battery staple tempo")}
	plain := []byte("segredo")

	ct := seal(t, c, k, plain)
	got, err := open(t, c, k, ct)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("round trip mismatch: %q", got)
	}
}

func TestAgeWrongPassphraseFails(t *testing.T) {
	c, _ := codec.GetCipher("age")
	ct := seal(t, c, &codec.Keyring{Passphrase: []byte("senha certa muito longa")}, []byte("x"))

	if _, err := open(t, c, &codec.Keyring{Passphrase: []byte("senha errada muito longa")}, ct); err == nil {
		t.Error("expected failure with wrong passphrase")
	}
}

// ---------- age: X25519 mode ----------

func TestAgeX25519RoundTripInlineIdentity(t *testing.T) {
	c, _ := codec.GetCipher("age")
	id, rcpt := newIdentity(t)
	plain := []byte("conteudo do backup")

	ct := seal(t, c, &codec.Keyring{Recipients: []string{rcpt}}, plain)
	got, err := open(t, c, &codec.Keyring{Identities: []string{id}}, ct)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Error("round trip mismatch")
	}
}

func TestAgeX25519RoundTripIdentityFile(t *testing.T) {
	c, _ := codec.GetCipher("age")
	id, rcpt := newIdentity(t)
	path := writeIdentityFile(t, t.TempDir(), "key.txt", id, 0o600)

	ct := seal(t, c, &codec.Keyring{Recipients: []string{rcpt}}, []byte("abc"))
	got, err := open(t, c, &codec.Keyring{IdentityFiles: []string{path}}, ct)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != "abc" {
		t.Errorf("got %q", got)
	}
}

// A recovery key kept offline is the documented protection against lockout,
// so any recipient must independently be able to open the archive.
func TestAgeMultipleRecipientsEachCanOpen(t *testing.T) {
	c, _ := codec.GetCipher("age")
	daily, dailyRcpt := newIdentity(t)
	recovery, recoveryRcpt := newIdentity(t)

	ct := seal(t, c, &codec.Keyring{Recipients: []string{dailyRcpt, recoveryRcpt}}, []byte("dados"))

	for name, id := range map[string]string{"daily": daily, "recovery": recovery} {
		got, err := open(t, c, &codec.Keyring{Identities: []string{id}}, ct)
		if err != nil {
			t.Fatalf("%s identity could not open: %v", name, err)
		}
		if string(got) != "dados" {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

// This test locks a load-bearing premise of the whole design: the age spec
// forbids combining a passphrase (scrypt) recipient with public-key recipients,
// which is why encryption mode is chosen per run rather than combined.
func TestAgeRejectsPassphraseCombinedWithRecipients(t *testing.T) {
	c, _ := codec.GetCipher("age")
	_, rcpt := newIdentity(t)

	k := &codec.Keyring{
		Passphrase: []byte("uma passphrase bem longa aqui"),
		Recipients: []string{rcpt},
	}
	var buf bytes.Buffer
	if _, err := c.Seal(&buf, k); err == nil {
		t.Fatal("expected Seal to reject passphrase combined with X25519 recipients")
	} else if !strings.Contains(strings.ToLower(err.Error()), "passphrase") {
		t.Errorf("error should explain the passphrase conflict, got: %v", err)
	}
}

func TestAgeRejectsEmptyKeyring(t *testing.T) {
	c, _ := codec.GetCipher("age")
	var buf bytes.Buffer
	if _, err := c.Seal(&buf, &codec.Keyring{}); err == nil {
		t.Error("expected Seal to reject a keyring with no key material")
	}
	if _, err := c.Open(bytes.NewReader(nil), &codec.Keyring{}); err == nil {
		t.Error("expected Open to reject a keyring with no key material")
	}
}

// A world-readable private key is a real finding, not a warning.
func TestAgeRefusesWorldReadableIdentityFile(t *testing.T) {
	c, _ := codec.GetCipher("age")
	id, rcpt := newIdentity(t)
	path := writeIdentityFile(t, t.TempDir(), "leaky.txt", id, 0o644)

	ct := seal(t, c, &codec.Keyring{Recipients: []string{rcpt}}, []byte("x"))
	_, err := open(t, c, &codec.Keyring{IdentityFiles: []string{path}}, ct)
	if err == nil {
		t.Fatal("expected refusal to use an identity file with permissive mode")
	}
	if !strings.Contains(err.Error(), "0644") {
		t.Errorf("error should name the offending mode, got: %v", err)
	}
}

// ---------- age: integrity ----------

func TestAgeDetectsTamperedPayload(t *testing.T) {
	c, _ := codec.GetCipher("age")
	k := &codec.Keyring{Passphrase: []byte("uma passphrase bem longa aqui")}
	ct := seal(t, c, k, bytes.Repeat([]byte("dados sensiveis "), 1024))

	tampered := bytes.Clone(ct)
	tampered[len(tampered)-8] ^= 0x01

	if _, err := open(t, c, k, tampered); err == nil {
		t.Error("expected AEAD to reject a flipped payload bit")
	}
}

// Truncation detection is why the design dropped the external checksum: the
// STREAM construction marks the final chunk, so a cut file cannot read clean.
func TestAgeDetectsTruncation(t *testing.T) {
	c, _ := codec.GetCipher("age")
	k := &codec.Keyring{Passphrase: []byte("uma passphrase bem longa aqui")}
	ct := seal(t, c, k, bytes.Repeat([]byte("dados sensiveis "), 8192))

	if _, err := open(t, c, k, ct[:len(ct)-64]); err == nil {
		t.Error("expected truncated archive to fail authentication")
	}
}

func TestAgeSealEmitsMagic(t *testing.T) {
	c, _ := codec.GetCipher("age")
	ct := seal(t, c, &codec.Keyring{Passphrase: []byte("uma passphrase bem longa aqui")}, []byte("x"))

	if !bytes.HasPrefix(ct, c.Magic()) {
		t.Errorf("output does not start with age magic: %q", ct[:24])
	}
	if want := "age-encryption.org/v1\n"; string(c.Magic()) != want {
		t.Errorf("Magic() = %q, want %q", c.Magic(), want)
	}
}

// ---------- detection by magic ----------

func TestDetectCipherAndCompressorByMagic(t *testing.T) {
	ageC, _ := codec.GetCipher("age")
	zstdC, _ := codec.GetCompressor("zstd")

	ct := seal(t, ageC, &codec.Keyring{Passphrase: []byte("uma passphrase bem longa aqui")}, []byte("x"))
	got, ok := codec.DetectCipher(ct)
	if !ok || got.Name() != "age" {
		t.Errorf("DetectCipher = %v, %v; want age, true", got, ok)
	}

	var zbuf bytes.Buffer
	w, _ := zstdC.NewWriter(&zbuf, codec.CompressOpts{Level: codec.LevelBest})
	w.Write([]byte("x"))
	w.Close()
	gotZ, ok := codec.DetectCompressor(zbuf.Bytes())
	if !ok || gotZ.Name() != "zstd" {
		t.Errorf("DetectCompressor = %v, %v; want zstd, true", gotZ, ok)
	}
}

func TestDetectRejectsUnknownAndShortInput(t *testing.T) {
	if _, ok := codec.DetectCipher([]byte("not an archive at all")); ok {
		t.Error("DetectCipher matched unknown data")
	}
	if _, ok := codec.DetectCompressor([]byte{0x00}); ok {
		t.Error("DetectCompressor matched a 1-byte input")
	}
	if _, ok := codec.DetectCipher(nil); ok {
		t.Error("DetectCipher matched nil")
	}
}

// Callers peek this many bytes before detecting, so it must cover every magic.
func TestMaxMagicLenCoversRegisteredCodecs(t *testing.T) {
	ageC, _ := codec.GetCipher("age")
	if codec.MaxMagicLen() < len(ageC.Magic()) {
		t.Errorf("MaxMagicLen() = %d, too small for age magic (%d)", codec.MaxMagicLen(), len(ageC.Magic()))
	}
}

// Validate must reach the same verdicts as Seal without doing the expensive
// key derivation, so callers can fail before creating an output file.
func TestValidateMatchesSealVerdicts(t *testing.T) {
	c, _ := codec.GetCipher("age")
	_, rcpt := newIdentity(t)

	cases := map[string]struct {
		k       *codec.Keyring
		wantErr bool
	}{
		"passphrase only": {&codec.Keyring{Passphrase: []byte("uma passphrase bem longa")}, false},
		"recipient only":  {&codec.Keyring{Recipients: []string{rcpt}}, false},
		"both modes":      {&codec.Keyring{Passphrase: []byte("x"), Recipients: []string{rcpt}}, true},
		"empty":           {&codec.Keyring{}, true},
		"bad recipient":   {&codec.Keyring{Recipients: []string{"not-a-key"}}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			validateErr := c.Validate(tc.k)
			if (validateErr != nil) != tc.wantErr {
				t.Fatalf("Validate err = %v, wantErr = %v", validateErr, tc.wantErr)
			}
			var buf bytes.Buffer
			_, sealErr := c.Seal(&buf, tc.k)
			if (sealErr != nil) != (validateErr != nil) {
				t.Errorf("Validate and Seal disagree: validate=%v seal=%v", validateErr, sealErr)
			}
		})
	}
}
