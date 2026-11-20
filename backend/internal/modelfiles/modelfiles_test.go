package modelfiles

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

const bucketPrefix = "https://bucket.example.org/"

type bucket struct{}

func (bucket) StoredKey(uri string) (string, bool) {
	return strings.CutPrefix(uri, bucketPrefix)
}

func (bucket) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not used")
}

func ptr[T any](value T) *T {
	return &value
}

func Test_should_find_the_object_to_read_when_a_file_is_ours_or_copied(t *testing.T) {
	cases := map[string]struct {
		location Location
		wantKey  string
		wantFrom ReadFrom
	}{
		"own file": {
			location: Location{URI: ptr(bucketPrefix + "e1/artifacts/a1/model.cif")},
			wantKey:  "e1/artifacts/a1/model.cif", wantFrom: ReadFromOwnFile,
		},
		"checksum copy": {
			location: Location{
				URI: ptr("https://files.rcsb.org/download/1ABC.cif"), CopyStatus: ptr(models.CopyStatusCopied),
				StoredURI: ptr(bucketPrefix + "copies/1ABC.cif"),
			},
			wantKey: "copies/1ABC.cif", wantFrom: ReadFromChecksumCopy,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			key, from, err := StoredKey(bucket{}, testCase.location)

			// then
			require.NoError(t, err)
			assert.Equal(t, testCase.wantKey, key)
			assert.Equal(t, testCase.wantFrom, from)
		})
	}
}

func Test_should_fail_for_good_when_a_file_has_no_object_in_our_bucket(t *testing.T) {
	external := ptr("https://files.rcsb.org/download/1ABC.cif")
	cases := map[string]Location{
		"no uri":                      {},
		"blank uri":                   {URI: ptr("  ")},
		"not copied yet":              {URI: external},
		"copied without a stored uri": {URI: external, CopyStatus: ptr(models.CopyStatusCopied)},
		"copy outside our bucket":     {URI: external, CopyStatus: ptr(models.CopyStatusCopied), StoredURI: external},
		"over the copy limit":         {URI: external, CopyStatus: ptr(models.CopyStatusOverLimit)},
		"copy off":                    {URI: external, CopyStatus: ptr(models.CopyStatusCopyOff)},
	}
	for name, location := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, _, err := StoredKey(bucket{}, location)

			// then
			require.Error(t, err)
			assert.True(t, IsPermanent(err), err.Error())
		})
	}
}

func Test_should_retry_when_the_copy_status_is_one_the_code_does_not_know(t *testing.T) {
	// when
	_, _, err := StoredKey(bucket{}, Location{URI: ptr("https://files.rcsb.org/x.cif"), CopyStatus: ptr(models.CopyStatus("lost"))})

	// then
	require.Error(t, err)
	assert.False(t, IsPermanent(err))
}

func gzipped(t *testing.T, text string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, err := writer.Write([]byte(text))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func Test_should_return_the_text_and_format_when_a_structure_file_is_opened(t *testing.T) {
	mmcif := "# comment\n\ndata_1ABC\n_entry.id 1ABC\n"
	pdb := "HEADER    TEST\nATOM      1  CA  ALA A   1\n"
	cases := map[string]struct {
		contents   []byte
		declared   *string
		wantFormat cif.StructureFormat
		wantText   string
	}{
		"mmCIF sniffed":             {contents: []byte(mmcif), wantFormat: cif.FormatMMCIF, wantText: mmcif},
		"PDB sniffed":               {contents: []byte(pdb), wantFormat: cif.FormatPDB, wantText: pdb},
		"gzipped mmCIF sniffed":     {contents: gzipped(t, mmcif), wantFormat: cif.FormatMMCIF, wantText: mmcif},
		"declared ent":              {contents: []byte(pdb), declared: ptr(" ENT "), wantFormat: cif.FormatPDB, wantText: pdb},
		"declared cif over sniff":   {contents: []byte(pdb), declared: ptr("cif"), wantFormat: cif.FormatMMCIF, wantText: pdb},
		"blank declared is sniffed": {contents: []byte(mmcif), declared: ptr(" "), wantFormat: cif.FormatMMCIF, wantText: mmcif},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			body, format, err := OpenStructure(bytes.NewReader(testCase.contents), testCase.declared)

			// then
			require.NoError(t, err)
			assert.Equal(t, testCase.wantFormat, format)
			text, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantText, string(text))
		})
	}
}

func Test_should_fail_for_good_when_a_file_is_no_structure_file(t *testing.T) {
	cases := map[string]struct {
		contents []byte
		declared *string
	}{
		"unknown declared format": {contents: []byte("data_X\n"), declared: ptr("xyz")},
		"neither format":          {contents: []byte("hello\n")},
		"empty":                   {contents: []byte("\n\n")},
		"broken gzip header":      {contents: []byte{0x1f, 0x8b, 0x00}},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, _, err := OpenStructure(bytes.NewReader(testCase.contents), testCase.declared)

			// then
			require.Error(t, err)
			assert.True(t, IsPermanent(err), err.Error())
		})
	}
}

func Test_should_find_a_permanent_error_when_it_is_wrapped(t *testing.T) {
	// given
	err := fmt.Errorf("read: %w", Permanent("bad file %d", 7))

	// then
	assert.True(t, IsPermanent(err))
	assert.Equal(t, "read: bad file 7", err.Error())
	assert.False(t, IsPermanent(errors.New("bad file")))
}
