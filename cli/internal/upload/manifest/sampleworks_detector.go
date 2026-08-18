package manifest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const sampleWorksInputPattern = "/mnt/diffuse-shared/raw/sampleworks/initial_dataset_40_occ_sweeps/processed/{{ pdb_id }}/{{ pdb_id }}_single_001_density_input.cif"

var sampleWorksRunDirPattern = regexp.MustCompile(`(?i)(?:^|/)([0-9][a-z0-9]{3})_([0-9]+(?:\.[0-9]+)?occ[A-Za-z](?:_[0-9]+(?:\.[0-9]+)?occ[A-Za-z])?)(?:/|$)`)

type SampleWorksDetector struct{}

type SampleWorksPattern struct {
	DensityMapPattern   string
	RefinedModelPattern string
	RunLogPattern       string
}

type sampleWorksDetectedRun struct {
	Dir     string
	PDBID   string
	Variant string
}

func (SampleWorksDetector) Detect(root string) ([]SampleWorksPattern, bool, error) {
	patterns, _, _, ok, err := detectSampleWorks(root)
	return patterns, ok, err
}

func detectSampleWorks(root string) ([]SampleWorksPattern, []sampleWorksDetectedRun, int, bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, 0, false, fmt.Errorf("resolve data folder path: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, nil, 0, false, fmt.Errorf("stat data folder: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, 0, false, fmt.Errorf("%s is not a directory", absRoot)
	}

	runs := make([]sampleWorksDetectedRun, 0)
	fileCount := 0
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", path, walkErr)
		}
		if !entry.IsDir() {
			fileCount++
			return nil
		}
		if path != absRoot && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		ok, err := isSampleWorksRunDir(path)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		relDir, err := filepath.Rel(absRoot, path)
		if err != nil {
			return fmt.Errorf("build Sampleworks run path for %s: %w", path, err)
		}
		relDir = filepath.ToSlash(relDir)
		pdbID, variant, ok := sampleWorksRunKey(relDir)
		if !ok {
			pdbID, variant, ok = sampleWorksRunKey(filepath.ToSlash(path))
		}
		if !ok {
			return nil
		}
		runs = append(runs, sampleWorksDetectedRun{
			Dir:     relDir,
			PDBID:   strings.ToLower(pdbID),
			Variant: variant,
		})
		return nil
	})
	if err != nil {
		return nil, nil, 0, false, fmt.Errorf("detect Sampleworks runs: %w", err)
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].PDBID != runs[j].PDBID {
			return runs[i].PDBID < runs[j].PDBID
		}
		if runs[i].Variant != runs[j].Variant {
			return runs[i].Variant < runs[j].Variant
		}
		return runs[i].Dir < runs[j].Dir
	})
	if len(runs) == 0 {
		return nil, nil, fileCount, false, nil
	}
	return sampleWorksPatterns(absRoot, runs), runs, fileCount, true, nil
}

func isSampleWorksRunDir(dir string) (bool, error) {
	for _, file := range []string{"job_metadata.json", "losses.txt", "refined.cif", "run.log"} {
		info, err := os.Stat(filepath.Join(dir, file))
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, fmt.Errorf("stat Sampleworks signature file %s: %w", filepath.Join(dir, file), err)
		}
		if info.IsDir() {
			return false, nil
		}
	}
	info, err := os.Stat(filepath.Join(dir, "trajectory"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat Sampleworks trajectory folder %s: %w", filepath.Join(dir, "trajectory"), err)
	}
	return info.IsDir(), nil
}

func sampleWorksRunKey(path string) (string, string, bool) {
	matches := sampleWorksRunDirPattern.FindStringSubmatch(path)
	if len(matches) != 3 {
		return "", "", false
	}
	return matches[1], matches[2], true
}

func sampleWorksPatterns(absRoot string, runs []sampleWorksDetectedRun) []SampleWorksPattern {
	root := filepath.ToSlash(absRoot)
	runDirs := map[string]struct{}{}
	for _, run := range runs {
		runDirs[sampleWorksTemplateSource(run.Dir, run.PDBID)] = struct{}{}
	}
	patterns := make([]SampleWorksPattern, 0, len(runDirs))
	for runDir := range runDirs {
		patterns = append(patterns, SampleWorksPattern{
			DensityMapPattern:   sampleWorksInputPattern,
			RefinedModelPattern: sampleWorksOutputFilePattern(root, runDir, "refined.cif"),
			RunLogPattern:       sampleWorksOutputFilePattern(root, runDir, "run.log"),
		})
	}
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].RefinedModelPattern < patterns[j].RefinedModelPattern
	})
	return patterns
}

func sampleWorksOutputFilePattern(root string, runDir string, fileName string) string {
	if strings.TrimSpace(runDir) == "" {
		return root + "/" + fileName
	}
	return root + "/" + runDir + "/" + fileName
}

func sampleWorksTemplateSource(source string, pdbID string) string {
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(pdbID))
	return re.ReplaceAllString(source, templatePDBID)
}
