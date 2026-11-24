package downloadfiles

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

const storageBase = "https://files.example/"

type storage struct{}

func (storage) StoredKey(uri string) (string, bool) {
	key, found := strings.CutPrefix(uri, storageBase)
	return key, found && key != ""
}

func file(uri *string, uploadedBy uuid.UUID) models.SelectionFile {
	return models.SelectionFile{ID: uuid.New(), URI: uri, UploadedBy: uploadedBy}
}

func ptr(value string) *string {
	return &value
}

func Test_should_give_each_file_the_reuse_terms_of_its_source_and_uploader(t *testing.T) {
	prism, depositor := uuid.New(), uuid.New()
	system := uuid.MustParse(models.SystemUserID)
	depositors := DepositorSet([]uuid.UUID{prism})
	cases := map[string]struct {
		file       models.SelectionFile
		wantSource Source
		wantTerms  models.DownloadReuseTerms
	}{
		"a PDB archive file":                   {file(ptr("https://files.rcsb.org/download/1ABC.cif"), depositor), SourcePDBArchive, models.DownloadReuseTermsCC0},
		"a PDB sequence":                       {file(ptr("https://www.rcsb.org/fasta/entry/1ABC"), depositor), SourcePDBArchive, models.DownloadReuseTermsCC0},
		"Prism's upload":                       {file(ptr(storageBase+"e1/model.cif"), prism), SourceDynamicPDBUpload, models.DownloadReuseTermsCC0},
		"Dynamic PDB's own (the sync account)": {file(ptr(storageBase+"e1/model.cif"), system), SourceDynamicPDBUpload, models.DownloadReuseTermsCC0},
		"another depositor's upload":           {file(ptr(storageBase+"e1/model.cif"), depositor), SourceDynamicPDBUpload, models.DownloadReuseTermsNotConfirmed},
		"another repository's file":            {file(ptr("https://zenodo.example/data.tar"), prism), SourceExternal, models.DownloadReuseTermsRepository},
		"a file without a location":            {file(nil, prism), SourceExternal, models.DownloadReuseTermsRepository},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			source := SourceOf(testCase.file, storage{})
			terms, err := ReuseTermsOf(testCase.file, source, depositors)

			// then
			require.NoError(t, err)
			assert.Equal(t, testCase.wantSource, source)
			assert.Equal(t, testCase.wantTerms, terms)
		})
	}
}

func Test_should_fail_on_an_unknown_source_when_reuse_terms_are_decided(t *testing.T) {
	_, err := ReuseTermsOf(file(nil, uuid.New()), Source("elsewhere"), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown file source: elsewhere")
}
