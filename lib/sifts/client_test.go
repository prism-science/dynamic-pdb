package sifts

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_get_uniprot_release_from_sifts_xml(t *testing.T) {
	// given
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	_, err := gzipWriter.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<entry xmlns="http://www.ebi.ac.uk/pdbe/docs/sifts/eFamily.xsd">
  <listDB>
    <db dbSource="PDB" dbVersion="2026-08-01"/>
    <db dbSource="UniProt" dbVersion="2026.03"/>
  </listDB>
</entry>`))
	require.NoError(t, err)
	require.NoError(t, gzipWriter.Close())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/am/5amf.xml.gz" {
			http.NotFound(w, r)
			return
		}
		_, err := w.Write(compressed.Bytes())
		require.NoError(t, err)
	}))
	defer server.Close()
	client := NewClient(WithBaseURL(server.URL))

	// when
	release, err := client.GetUniProtRelease(context.Background(), "5AMF")

	// then
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Equal(t, "2026.03", *release)
}

func Test_should_return_no_uniprot_release_when_sifts_xml_is_missing(t *testing.T) {
	// given
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := NewClient(WithBaseURL(server.URL))

	// when
	release, err := client.GetUniProtRelease(context.Background(), "5AMF")

	// then
	require.NoError(t, err)
	assert.Nil(t, release)
}
