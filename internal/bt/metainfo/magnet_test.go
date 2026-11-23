package metainfo

import (
	"encoding/hex"
	"slices"
	"testing"

	qt "github.com/go-quicktest/qt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	exampleMagnetURI = `magnet:?xt=urn:btih:51340689c960f0778a4387aef9b4b52fd08390cd&dn=Shit+Movie+%281985%29+1337p+-+Eru&tr=http%3A%2F%2Fhttp.was.great%21&tr=udp%3A%2F%2Fanti.piracy.honeypot%3A6969`
	exampleMagnet    = Magnet{
		DisplayName: "Shit Movie (1985) 1337p - Eru",
		Trackers: []string{
			"http://http.was.great!",
			"udp://anti.piracy.honeypot:6969",
		},
	}
)

func init() {
	hex.Decode(exampleMagnet.InfoHash[:], []byte("51340689c960f0778a4387aef9b4b52fd08390cd"))
}

func TestMagnetString(t *testing.T) {
	m, err := ParseMagnetUri(exampleMagnet.String())
	require.NoError(t, err)
	assert.EqualValues(t, exampleMagnet, m)
}

func TestParseMagnetURI(t *testing.T) {
	var uri string
	var m Magnet
	var err error

	uri = "magnet:?xt=urn:btih:ZOCMZQIPFFW7OLLMIC5HUB6BPCSDEOQU"
	_, err = ParseMagnetUri(uri)
	if err != nil {
		t.Errorf("Attempting parsing the proper Magnet btih URI:\"%v\" failed with err: %v", uri, err)
	}

	m, err = ParseMagnetUri(exampleMagnetURI)
	assert.EqualValues(t, exampleMagnet, m)
	assert.NoError(t, err)

	_, err = ParseMagnetUri("")
	if err == nil {
		t.Errorf("Parsing empty string as URI should have returned an error but didn't")
	}

	uri = "magnet:?xt=urn:sha1:YNCKHTQCWBTRNJIV4WNAE52SJUQCZO5C"
	_, err = ParseMagnetUri(uri)
	if err == nil {
		t.Errorf("Magnet URI with non-BTIH URNs (like \"%v\") are not supported and should return an error", uri)
	}

	uri = "magnet:?xt=urn:btih:this hash is really broken"
	_, err = ParseMagnetUri(uri)
	if err == nil {
		t.Errorf("Failed to detect broken Magnet URI: %v", uri)
	}
}

func TestPaddedBase32MagnetIsAnError(t *testing.T) {
	assert.NotPanics(t, func() {
		_, err := ParseMagnetUri("magnet:?xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
		assert.Error(t, err)
	})
}

func TestMagnetEdgeCases(t *testing.T) {
	upper, err := ParseMagnetUri("magnet:?xt=urn:btih:CI2UACNJ4CA7G2J3QWNPX6EH2DPCGF6D")
	require.NoError(t, err)
	lower, err := ParseMagnetUri("magnet:?xt=urn:btih:ci2uacnj4ca7g2j3qwnpx6eh2dpcgf6d")
	require.NoError(t, err)
	assert.Equal(t, upper.InfoHash, lower.InfoHash)
	_, err = ParseMagnetV2Uri("magnet:?dn=nothing")
	assert.Error(t, err)
}

func TestMagnetize(t *testing.T) {
	mi, err := LoadFromFile("../testdata/bootstrap.dat.torrent")
	require.NoError(t, err)

	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	m := mi.Magnet(nil, &info)

	assert.EqualValues(t, "bootstrap.dat", m.DisplayName)

	ih := [20]byte{
		54, 113, 155, 162, 206, 207, 159, 59, 215, 197,
		171, 251, 122, 136, 233, 57, 97, 27, 83, 108,
	}

	if m.InfoHash != ih {
		t.Errorf("Magnet infohash is incorrect")
	}

	trackers := []string{
		"udp://tracker.openbittorrent.com:80",
		"udp://tracker.openbittorrent.com:80",
		"udp://tracker.publicbt.com:80",
		"udp://coppersurfer.tk:6969/announce",
		"udp://open.demonii.com:1337",
		"http://bttracker.crunchbanglinux.org:6969/announce",
	}

	for _, expected := range trackers {
		if !contains(m.Trackers, expected) {
			t.Errorf("Magnet does not contain expected tracker: %s", expected)
		}
	}
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

func TestParseSintelMagnet(t *testing.T) {
	mi, err := LoadFromFile("../testdata/sintel.torrent")
	qt.Assert(t, qt.IsNil(err))
	m := mi.Magnet(nil, nil)
	ms := m.String()
	t.Logf("magnet link: %q", ms)
	m, err = ParseMagnetUri(ms)
	qt.Check(t, qt.IsNil(err))
	m2, err := ParseMagnetV2Uri(ms)
	qt.Check(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(m2.InfoHash.Value, m.InfoHash))
}
