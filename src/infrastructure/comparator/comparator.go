package comparator

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

type Comparator struct{}

func (c *Comparator) Compare(expectedPath string, outputPath string) (int, error) {
	return c.CompareZoj(expectedPath, outputPath)
}

func (c *Comparator) CompareZoj(expectedPath, outputPath string) (int, error) {
	expectedBytes, err1 := os.ReadFile(expectedPath)
	if err1 != nil {
		return domain.OJ_RE, fmt.Errorf("error reading expected output: %w", err1)
	}

	outputBytes, err2 := os.ReadFile(outputPath)
	if err2 != nil {
		return domain.OJ_RE, fmt.Errorf("error reading user output: %w", err2)
	}

	// Compare directly first
	if bytes.Equal(bytes.TrimRight(expectedBytes, "\r\n \t"), bytes.TrimRight(outputBytes, "\r\n \t")) {
		return domain.OJ_AC, nil
	}

	// Normalize whitespace for PE check
	expectedNorm := normalizeWhitespace(expectedBytes)
	outputNorm := normalizeWhitespace(outputBytes)

	if expectedNorm == outputNorm {
		return domain.OJ_PE, nil
	}

	return domain.OJ_WA, nil
}

func normalizeWhitespace(data []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(bufio.ScanWords)

	var builder strings.Builder
	first := true
	for scanner.Scan() {
		if !first {
			builder.WriteRune(' ')
		}
		builder.WriteString(scanner.Text())
		first = false
	}
	return builder.String()
}
