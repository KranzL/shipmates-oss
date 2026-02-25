package testgen

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

func ParseLcov(coverageFilePath string) (*CoverageReport, error) {
	file, err := os.Open(coverageFilePath)
	if err != nil {
		return nil, fmt.Errorf("open lcov file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat lcov file: %w", err)
	}
	if stat.Size() > MaxCoverageFileSize {
		return nil, fmt.Errorf("lcov file exceeds max size: %d > %d", stat.Size(), MaxCoverageFileSize)
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	fileCoverage := make(map[string]*FileCoverageDetail)
	uncoveredLines := make(map[string][]int)

	var currentFile string
	var linesCovered int
	var linesTotal int
	currentUncovered := []int{}

	fileCount := 0

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "SF:") {
			if currentFile != "" {
				fileCoverage[currentFile] = &FileCoverageDetail{
					Path:         currentFile,
					LinesCovered: linesCovered,
					LinesTotal:   linesTotal,
				}
				if len(currentUncovered) > 0 {
					uncoveredLines[currentFile] = currentUncovered
				}
			}

			fileCount++
			if fileCount > MaxFilesToParse {
				break
			}

			currentFile = strings.TrimPrefix(line, "SF:")
			linesCovered = 0
			linesTotal = 0
			currentUncovered = []int{}
			continue
		}

		if strings.HasPrefix(line, "DA:") {
			parts := strings.SplitN(strings.TrimPrefix(line, "DA:"), ",", 2)
			if len(parts) != 2 {
				continue
			}

			lineNum, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}

			hitCount, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}

			linesTotal++
			if hitCount > 0 {
				linesCovered++
			} else {
				currentUncovered = append(currentUncovered, lineNum)
			}
			continue
		}

		if line == "end_of_record" {
			if currentFile != "" {
				fileCoverage[currentFile] = &FileCoverageDetail{
					Path:         currentFile,
					LinesCovered: linesCovered,
					LinesTotal:   linesTotal,
				}
				if len(currentUncovered) > 0 {
					uncoveredLines[currentFile] = currentUncovered
				}
			}
			currentFile = ""
			linesCovered = 0
			linesTotal = 0
			currentUncovered = []int{}
			continue
		}
	}

	if currentFile != "" {
		fileCoverage[currentFile] = &FileCoverageDetail{
			Path:         currentFile,
			LinesCovered: linesCovered,
			LinesTotal:   linesTotal,
		}
		if len(currentUncovered) > 0 {
			uncoveredLines[currentFile] = currentUncovered
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan lcov file: %w", err)
	}

	for path, detail := range fileCoverage {
		if detail.LinesTotal > 0 {
			detail.CoveragePercent = float64(detail.LinesCovered) / float64(detail.LinesTotal) * 100.0
		}
		if uncovered, exists := uncoveredLines[path]; exists {
			detail.UncoveredLineRanges = CollapseLineRanges(uncovered)
		}
	}

	totalCoverage := CalculateTotalCoverage(fileCoverage)

	return &CoverageReport{
		TotalCoverage:  totalCoverage,
		FileCoverage:   fileCoverage,
		UncoveredLines: uncoveredLines,
	}, nil
}

func ParseGoCoverage(coverageFilePath string) (*CoverageReport, error) {
	file, err := os.Open(coverageFilePath)
	if err != nil {
		return nil, fmt.Errorf("open go coverage file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat go coverage file: %w", err)
	}
	if stat.Size() > MaxCoverageFileSize {
		return nil, fmt.Errorf("go coverage file exceeds max size: %d > %d", stat.Size(), MaxCoverageFileSize)
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	fileCoverage := make(map[string]*FileCoverageDetail)
	uncoveredLines := make(map[string][]int)

	fileCount := 0
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		if lineNum == 1 {
			if !strings.HasPrefix(line, "mode:") {
				return nil, fmt.Errorf("invalid go coverage format: missing mode header")
			}
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		fileParts := strings.SplitN(parts[0], ":", 2)
		if len(fileParts) != 2 {
			continue
		}

		filePath := fileParts[0]
		rangeSpec := fileParts[1]

		rangeParts := strings.Split(rangeSpec, ",")
		if len(rangeParts) != 2 {
			continue
		}

		startParts := strings.Split(rangeParts[0], ".")
		endParts := strings.Split(rangeParts[1], ".")
		if len(startParts) < 1 || len(endParts) < 1 {
			continue
		}

		startLine, err := strconv.Atoi(startParts[0])
		if err != nil {
			continue
		}

		endLine, err := strconv.Atoi(endParts[0])
		if err != nil {
			continue
		}

		count, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		if _, exists := fileCoverage[filePath]; !exists {
			fileCount++
			if fileCount > MaxFilesToParse {
				break
			}
			fileCoverage[filePath] = &FileCoverageDetail{
				Path: filePath,
			}
		}

		detail := fileCoverage[filePath]
		lineCount := endLine - startLine + 1
		detail.LinesTotal += lineCount

		if count > 0 {
			detail.LinesCovered += lineCount
		} else {
			for i := startLine; i <= endLine; i++ {
				uncoveredLines[filePath] = append(uncoveredLines[filePath], i)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan go coverage file: %w", err)
	}

	for path, detail := range fileCoverage {
		if detail.LinesTotal > 0 {
			detail.CoveragePercent = float64(detail.LinesCovered) / float64(detail.LinesTotal) * 100.0
		}
		if uncovered, exists := uncoveredLines[path]; exists {
			detail.UncoveredLineRanges = CollapseLineRanges(uncovered)
		}
	}

	totalCoverage := CalculateTotalCoverage(fileCoverage)

	return &CoverageReport{
		TotalCoverage:  totalCoverage,
		FileCoverage:   fileCoverage,
		UncoveredLines: uncoveredLines,
	}, nil
}

func CollapseLineRanges(lines []int) []LineRange {
	if len(lines) == 0 {
		return []LineRange{}
	}

	sorted := make([]int, len(lines))
	copy(sorted, lines)
	sort.Ints(sorted)

	ranges := []LineRange{}
	start := sorted[0]
	end := sorted[0]

	for i := 1; i < len(sorted); i++ {
		if sorted[i] == end+1 {
			end = sorted[i]
		} else {
			ranges = append(ranges, LineRange{Start: start, End: end})
			start = sorted[i]
			end = sorted[i]
		}
	}

	ranges = append(ranges, LineRange{Start: start, End: end})
	return ranges
}

func CalculateTotalCoverage(fileCoverage map[string]*FileCoverageDetail) float64 {
	totalCovered := 0
	totalLines := 0

	for _, detail := range fileCoverage {
		totalCovered += detail.LinesCovered
		totalLines += detail.LinesTotal
	}

	if totalLines == 0 {
		return 0.0
	}

	return float64(totalCovered) / float64(totalLines) * 100.0
}

func ExtractTopGapFiles(coverage *CoverageReport, limit int) []CoverageGap {
	gaps := []CoverageGap{}

	for _, detail := range coverage.FileCoverage {
		if detail.LinesTotal == 0 {
			continue
		}

		uncoveredCount := detail.LinesTotal - detail.LinesCovered

		gaps = append(gaps, CoverageGap{
			Path:            detail.Path,
			CoveragePercent: detail.CoveragePercent,
			UncoveredRanges: detail.UncoveredLineRanges,
			UncoveredCount:  uncoveredCount,
		})
	}

	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].CoveragePercent < gaps[j].CoveragePercent
	})

	if len(gaps) > limit {
		gaps = gaps[:limit]
	}

	return gaps
}
