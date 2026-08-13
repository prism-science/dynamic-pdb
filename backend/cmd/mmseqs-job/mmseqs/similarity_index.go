package mmseqs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type SimilarityIndexBuilder struct {
	commands *Commands
	cacheDir string
	runID    uuid.UUID
}

type SimilarityIndexBuildResult struct {
	SimilarityIndexPath string
	SearchResultPath    string
	SequenceCount       int64
	Hits                []Hit
}

func NewSimilarityIndexBuilder(commands *Commands, cacheDir string, runID uuid.UUID) (*SimilarityIndexBuilder, error) {
	if commands == nil {
		return nil, errors.New("mmseqs commands is nil")
	}
	if strings.TrimSpace(cacheDir) == "" {
		return nil, errors.New("mmseqs cache dir is empty")
	}
	if runID == uuid.Nil {
		return nil, errors.New("run id is empty")
	}
	return &SimilarityIndexBuilder{
		commands: commands,
		cacheDir: cacheDir,
		runID:    runID,
	}, nil
}

func (b *SimilarityIndexBuilder) BuildSimilarityIndex(
	ctx context.Context,
	existingSimilarityIndexPath string,
) (result *SimilarityIndexBuildResult, err error) {
	sequenceFilePath := b.sequenceFilePath()
	if exists, err := fileExists(sequenceFilePath); err != nil {
		return nil, fmt.Errorf("check sequence file: %w", err)
	} else if !exists {
		return nil, fmt.Errorf("sequence file does not exist: %s", sequenceFilePath)
	}

	existingSimilarityIndexPath = strings.TrimSpace(existingSimilarityIndexPath)
	if existingSimilarityIndexPath != "" {
		if exists, err := fileExists(existingSimilarityIndexPath); err != nil {
			return nil, fmt.Errorf("check existing similarity index file: %w", err)
		} else if !exists {
			return nil, fmt.Errorf("existing similarity index file does not exist: %s", existingSimilarityIndexPath)
		}
	}

	paths := b.similarityIndexBuildPaths(sequenceFilePath)
	defer func() {
		if cleanupErr := removePaths(paths.partialIndex.indexDir, paths.partialIndex.buildDir, paths.searchTmpDir, paths.searchResultTemp); cleanupErr != nil && err == nil {
			err = fmt.Errorf("cleanup similarity index build files: %w", cleanupErr)
		}
	}()

	if _, ok, err := b.existingSimilarityIndex(paths.partialIndex); err != nil {
		return nil, fmt.Errorf("check new partial similarity index: %w", err)
	} else if !ok {
		if _, err := b.buildSimilarityIndex(ctx, paths.partialIndex); err != nil {
			return nil, fmt.Errorf("build new partial similarity index: %w", err)
		}
	}

	if existingSimilarityIndexPath != "" {
		if err := b.searchNewSequences(ctx, existingSimilarityIndexPath, paths); err != nil {
			return nil, fmt.Errorf("search new sequences: %w", err)
		}
	} else if err := publishEmptyFile(paths.searchResultTemp, paths.searchResult); err != nil {
		return nil, fmt.Errorf("publish empty search result: %w", err)
	}

	fullIndex, ok, err := b.existingSimilarityIndex(paths.fullIndex)
	if err != nil {
		return nil, fmt.Errorf("check full similarity index: %w", err)
	}
	if !ok {
		fullIndex, err = b.publishSimilarityIndex(ctx, existingSimilarityIndexPath, paths)
		if err != nil {
			return nil, fmt.Errorf("publish similarity index: %w", err)
		}
	}

	hits, err := ParseOutput(paths.searchResult)
	if err != nil {
		return nil, fmt.Errorf("parse new sequence search result: %w", err)
	}
	return &SimilarityIndexBuildResult{
		SimilarityIndexPath: fullIndex.SimilarityIndexPath,
		SearchResultPath:    paths.searchResult,
		SequenceCount:       fullIndex.SequenceCount,
		Hits:                hits,
	}, nil
}

func (b *SimilarityIndexBuilder) searchNewSequences(
	ctx context.Context,
	existingSimilarityIndexPath string,
	paths similarityIndexBuildPaths,
) error {
	if exists, err := fileExists(paths.searchResult); err != nil {
		return fmt.Errorf("check search result file: %w", err)
	} else if exists {
		return nil
	}

	if err := os.RemoveAll(paths.searchTmpDir); err != nil {
		return fmt.Errorf("remove stale search tmp dir: %w", err)
	}
	if err := os.MkdirAll(paths.searchTmpDir, 0o755); err != nil {
		return fmt.Errorf("create search tmp dir: %w", err)
	}
	if err := removeIfExists(paths.searchResultTemp); err != nil {
		return fmt.Errorf("remove stale search result temp file: %w", err)
	}

	existingResultPath := filepath.Join(paths.searchTmpDir, "existing-index.tsv")
	partialResultPath := filepath.Join(paths.searchTmpDir, "partial-index.tsv")
	if err := b.commands.EasySearch(ctx, paths.partialIndex.sequenceFile, existingSimilarityIndexPath, existingResultPath, filepath.Join(paths.searchTmpDir, "existing-index.tmp")); err != nil {
		return fmt.Errorf("search new sequences against existing similarity index: %w", err)
	}
	if err := b.commands.EasySearch(ctx, paths.partialIndex.sequenceFile, paths.partialIndex.index, partialResultPath, filepath.Join(paths.searchTmpDir, "partial-index.tmp")); err != nil {
		return fmt.Errorf("search new sequences against partial similarity index: %w", err)
	}
	if err := combineSearchResults(paths.searchResultTemp, existingResultPath, partialResultPath); err != nil {
		return fmt.Errorf("combine search result files: %w", err)
	}
	if err := os.Rename(paths.searchResultTemp, paths.searchResult); err != nil {
		return fmt.Errorf("publish search result file: %w", err)
	}
	return nil
}

func (b *SimilarityIndexBuilder) publishSimilarityIndex(
	ctx context.Context,
	existingSimilarityIndexPath string,
	paths similarityIndexBuildPaths,
) (result *similarityIndexResult, err error) {
	partialIndex, ok, err := b.existingSimilarityIndex(paths.partialIndex)
	if err != nil {
		return nil, fmt.Errorf("check new partial similarity index: %w", err)
	}
	if !ok {
		return nil, errors.New("new partial similarity index is not built")
	}

	if err := os.RemoveAll(paths.fullIndex.buildDir); err != nil {
		return nil, fmt.Errorf("remove stale full similarity index build dir: %w", err)
	}
	if err := os.MkdirAll(paths.fullIndex.buildDir, 0o755); err != nil {
		return nil, fmt.Errorf("create full similarity index build dir: %w", err)
	}
	published := false
	defer func() {
		if published {
			return
		}
		if removeErr := os.RemoveAll(paths.fullIndex.buildDir); removeErr != nil && err == nil {
			err = fmt.Errorf("remove failed full similarity index build dir: %w", removeErr)
		}
	}()

	if existingSimilarityIndexPath == "" {
		if err := copyDirectory(paths.partialIndex.indexDir, paths.fullIndex.buildDir); err != nil {
			return nil, fmt.Errorf("copy partial similarity index: %w", err)
		}
	} else {
		if err := os.MkdirAll(paths.fullIndex.tmpDir, 0o755); err != nil {
			return nil, fmt.Errorf("create full similarity index tmp dir: %w", err)
		}
		if err := b.commands.ConcatDBs(ctx, existingSimilarityIndexPath, paths.partialIndex.index, paths.fullIndex.buildIndex); err != nil {
			return nil, fmt.Errorf("concat similarity index databases: %w", err)
		}
		if err := b.commands.CreateIndex(ctx, paths.fullIndex.buildIndex, paths.fullIndex.tmpDir); err != nil {
			return nil, fmt.Errorf("create full similarity index lookup files: %w", err)
		}
		if err := os.RemoveAll(paths.fullIndex.tmpDir); err != nil {
			return nil, fmt.Errorf("remove full similarity index tmp dir: %w", err)
		}
	}
	if err := os.Rename(paths.fullIndex.buildDir, paths.fullIndex.indexDir); err != nil {
		return nil, fmt.Errorf("publish full similarity index dir: %w", err)
	}
	published = true

	sequenceCount, err := countMMseqsIndexRecords(paths.fullIndex.index)
	if err != nil {
		return nil, fmt.Errorf("count full similarity index records: %w", err)
	}
	if sequenceCount < partialIndex.SequenceCount {
		return nil, errors.New("full similarity index has fewer records than new partial index")
	}
	return &similarityIndexResult{
		SimilarityIndexPath: paths.fullIndex.index,
		SequenceCount:       sequenceCount,
	}, nil
}

func (b *SimilarityIndexBuilder) buildSimilarityIndex(ctx context.Context, paths similarityIndexPaths) (result *similarityIndexResult, err error) {
	if result, ok, err := b.existingSimilarityIndex(paths); err != nil {
		return nil, fmt.Errorf("check existing similarity index: %w", err)
	} else if ok {
		return result, nil
	}

	sequenceCount, err := countFASTARecords(paths.sequenceFile)
	if err != nil {
		return nil, fmt.Errorf("count sequence file records: %w", err)
	}
	if sequenceCount == 0 {
		return nil, errors.New("sequence file does not contain records")
	}

	if err := os.RemoveAll(paths.buildDir); err != nil {
		return nil, fmt.Errorf("remove stale similarity index build dir: %w", err)
	}
	if err := os.MkdirAll(paths.buildDir, 0o755); err != nil {
		return nil, fmt.Errorf("create similarity index build dir: %w", err)
	}
	published := false
	defer func() {
		if published {
			return
		}
		if removeErr := os.RemoveAll(paths.buildDir); removeErr != nil && err == nil {
			err = fmt.Errorf("remove failed similarity index build dir: %w", removeErr)
		}
	}()

	if err := os.MkdirAll(paths.tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("create similarity index tmp dir: %w", err)
	}
	if err := b.commands.CreateDB(ctx, paths.sequenceFile, paths.buildIndex); err != nil {
		return nil, fmt.Errorf("create similarity index: %w", err)
	}
	if err := b.commands.CreateIndex(ctx, paths.buildIndex, paths.tmpDir); err != nil {
		return nil, fmt.Errorf("create similarity index lookup files: %w", err)
	}
	if err := os.RemoveAll(paths.tmpDir); err != nil {
		return nil, fmt.Errorf("remove similarity index tmp dir: %w", err)
	}
	if err := os.Rename(paths.buildDir, paths.indexDir); err != nil {
		return nil, fmt.Errorf("publish similarity index dir: %w", err)
	}
	published = true

	return &similarityIndexResult{
		SimilarityIndexPath: paths.index,
		SequenceCount:       sequenceCount,
	}, nil
}

func (b *SimilarityIndexBuilder) existingSimilarityIndex(paths similarityIndexPaths) (*similarityIndexResult, bool, error) {
	if exists, err := directoryExists(paths.indexDir); err != nil {
		return nil, false, fmt.Errorf("check similarity index dir: %w", err)
	} else if !exists {
		return nil, false, nil
	}

	if exists, err := fileExists(paths.index); err != nil {
		return nil, false, fmt.Errorf("check similarity index file: %w", err)
	} else if !exists {
		return nil, false, errors.New("similarity index dir exists without mmseqs index file")
	}

	sequenceCount, err := countMMseqsIndexRecords(paths.index)
	if err != nil {
		return nil, false, fmt.Errorf("count similarity index records: %w", err)
	}
	if sequenceCount == 0 {
		return nil, false, errors.New("similarity index does not contain records")
	}
	return &similarityIndexResult{
		SimilarityIndexPath: paths.index,
		SequenceCount:       sequenceCount,
	}, true, nil
}

func (b *SimilarityIndexBuilder) sequenceFilePath() string {
	return filepath.Join(b.cacheDir, "runs", b.runID.String(), "data", "sequences.fasta")
}

func (b *SimilarityIndexBuilder) similarityIndexBuildPaths(sequenceFilePath string) similarityIndexBuildPaths {
	runDir := filepath.Join(b.cacheDir, "runs", b.runID.String())
	return similarityIndexBuildPaths{
		partialIndex:     b.partialSimilarityIndexPaths(runDir, sequenceFilePath),
		fullIndex:        b.fullSimilarityIndexPaths(runDir),
		searchTmpDir:     filepath.Join(runDir, "similarity-search.tmp"),
		searchResult:     filepath.Join(runDir, "similarity-search.tsv"),
		searchResultTemp: filepath.Join(runDir, "similarity-search.tsv.tmp"),
	}
}

func (b *SimilarityIndexBuilder) partialSimilarityIndexPaths(runDir string, sequenceFile string) similarityIndexPaths {
	indexDir := filepath.Join(runDir, "partial-similarity-index")
	buildDir := filepath.Join(runDir, "partial-similarity-index.tmp")
	return similarityIndexPaths{
		sequenceFile: sequenceFile,
		indexDir:     indexDir,
		buildDir:     buildDir,
		tmpDir:       filepath.Join(buildDir, "tmp"),
		index:        filepath.Join(indexDir, "index"),
		buildIndex:   filepath.Join(buildDir, "index"),
	}
}

func (b *SimilarityIndexBuilder) fullSimilarityIndexPaths(runDir string) similarityIndexPaths {
	indexDir := filepath.Join(runDir, "similarity-index")
	buildDir := filepath.Join(runDir, "similarity-index.tmp")
	return similarityIndexPaths{
		indexDir:   indexDir,
		buildDir:   buildDir,
		tmpDir:     filepath.Join(buildDir, "tmp"),
		index:      filepath.Join(indexDir, "index"),
		buildIndex: filepath.Join(buildDir, "index"),
	}
}

type similarityIndexBuildPaths struct {
	partialIndex     similarityIndexPaths
	fullIndex        similarityIndexPaths
	searchTmpDir     string
	searchResult     string
	searchResultTemp string
}

type similarityIndexPaths struct {
	sequenceFile string
	indexDir     string
	buildDir     string
	tmpDir       string
	index        string
	buildIndex   string
}

type similarityIndexResult struct {
	SimilarityIndexPath string
	SequenceCount       int64
}

func copyDirectory(sourceDir string, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk source path: %w", walkErr)
		}
		relativePath, err := filepath.Rel(sourceDir, sourcePath)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}
		targetPath := filepath.Join(targetDir, relativePath)
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read source path info: %w", err)
		}
		if entry.IsDir() {
			if err := os.MkdirAll(targetPath, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create target dir: %w", err)
			}
			return nil
		}
		if err := copyFile(sourcePath, targetPath, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy file: %w", err)
		}
		return nil
	})
}

func copyFile(sourcePath string, targetPath string, mode fs.FileMode) (err error) {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() {
		if closeErr := sourceFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close source file: %w", closeErr)
		}
	}()

	targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open target file: %w", err)
	}
	closed := false
	defer func() {
		if closed {
			return
		}
		if closeErr := targetFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close target file: %w", closeErr)
		}
	}()

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return fmt.Errorf("copy file contents: %w", err)
	}
	if err := targetFile.Close(); err != nil {
		return fmt.Errorf("close target file: %w", err)
	}
	closed = true
	return nil
}

func publishEmptyFile(tempPath string, path string) error {
	if err := os.WriteFile(tempPath, []byte{}, 0o644); err != nil {
		return fmt.Errorf("write empty temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish empty file: %w", err)
	}
	return nil
}

func combineSearchResults(outputPath string, inputPaths ...string) (err error) {
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create combined search result file: %w", err)
	}
	closed := false
	defer func() {
		if closed {
			return
		}
		if closeErr := outputFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close combined search result file: %w", closeErr)
		}
	}()

	writer := bufio.NewWriter(outputFile)
	for _, inputPath := range inputPaths {
		if err := appendSearchResult(writer, inputPath); err != nil {
			return fmt.Errorf("append search result %s: %w", inputPath, err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush combined search result file: %w", err)
	}
	if err := outputFile.Close(); err != nil {
		return fmt.Errorf("close combined search result file: %w", err)
	}
	closed = true
	return nil
}

func appendSearchResult(writer *bufio.Writer, inputPath string) (err error) {
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open search result file: %w", err)
	}
	defer func() {
		if closeErr := inputFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close search result file: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(inputFile)
	scanner.Buffer(make([]byte, 1024), 32*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return fmt.Errorf("write search result line: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan search result file: %w", err)
	}
	return nil
}

func countFASTARecords(path string) (count int64, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open FASTA file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close FASTA file: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 32*1024*1024)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), ">") {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan FASTA file: %w", err)
	}
	return count, nil
}

func countMMseqsIndexRecords(databasePath string) (count int64, err error) {
	file, err := os.Open(databasePath + ".index")
	if err != nil {
		return 0, fmt.Errorf("open mmseqs database index file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close mmseqs database index file: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 32*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan mmseqs database index file: %w", err)
	}
	return count, nil
}

func removePaths(paths ...string) error {
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}

func directoryExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("path exists and is not a directory: %s", path)
	}
	return true, nil
}
