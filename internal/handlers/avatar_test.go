package handlers

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNameInitials(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Andi Saputra", "AS"},
		{"Budi", "B"},
		{"  Citra   Dewi  ", "CD"}, // spasi berlebih tetap kebaca benar
		{"", "?"},
	}
	for _, c := range cases {
		if got := nameInitials(c.name); got != c.want {
			t.Errorf("nameInitials(%q) = %q, mau %q", c.name, got, c.want)
		}
	}
}

func TestColorForName_Consistent(t *testing.T) {
	// Nama yang sama harus SELALU dapat warna yang sama tiap kali dipanggil.
	name := "Andi Saputra"
	first := colorForName(name)
	for i := 0; i < 5; i++ {
		if got := colorForName(name); got != first {
			t.Fatalf("warna berubah-ubah untuk nama yang sama: %q vs %q", first, got)
		}
	}
}

func TestInitialsAvatarDataURI_ValidDataURI(t *testing.T) {
	uri := initialsAvatarDataURI("Andi Saputra")

	const prefix = "data:image/svg+xml;base64,"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("hasil bukan data URI SVG yang valid, dapat: %s", uri[:min(50, len(uri))])
	}

	// Pastikan base64-nya beneran valid dan bisa di-decode balik.
	encoded := strings.TrimPrefix(uri, prefix)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("gagal decode base64: %v", err)
	}

	svg := string(decoded)
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "AS") {
		t.Fatalf("isi SVG tidak sesuai, dapat: %s", svg)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
