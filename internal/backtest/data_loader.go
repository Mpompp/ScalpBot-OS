package backtest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// LoadTicksFromCSV loads all ticks from a CSV file into memory using zero-allocation byte slicing.
// Supports both MT5 standard tick exports and generic timestamp,bid,ask formats.
func LoadTicksFromCSV(filePath string, symbol string) ([]model.Tick, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open tick file %s: %w", filePath, err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 512*1024) // 512KB fast I/O buffer
	ticks := make([]model.Tick, 0, 100000)

	lineNum := 0
	for {
		line, isPrefix, err := reader.ReadLine()
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("read error at line %d: %w", lineNum, err)
		}
		if isPrefix {
			// Line too long for single read, skip or handle
			continue
		}

		lineNum++
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Skip header line if present
		if lineNum == 1 && (bytes.Contains(line, []byte("Date")) || bytes.Contains(line, []byte("date")) || bytes.Contains(line, []byte("bid")) || bytes.Contains(line, []byte("Bid"))) {
			continue
		}

		tick, parseErr := parseTickLineBytes(line, symbol)
		if parseErr != nil {
			continue // Skip malformed lines gracefully
		}

		ticks = append(ticks, tick)
	}

	return ticks, nil
}

// StreamTicksFromCSV streams ticks from CSV directly to a channel using zero-allocation byte parsing.
func StreamTicksFromCSV(ctx context.Context, filePath string, symbol string, tickCh chan<- model.Tick) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open tick file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 512*1024)
	lineNum := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, isPrefix, err := reader.ReadLine()
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				break
			}
			return err
		}
		if isPrefix {
			continue
		}

		lineNum++
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if lineNum == 1 && (bytes.Contains(line, []byte("Date")) || bytes.Contains(line, []byte("bid"))) {
			continue
		}

		tick, parseErr := parseTickLineBytes(line, symbol)
		if parseErr != nil {
			continue
		}

		select {
		case tickCh <- tick:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// parseTickLineBytes parses a single CSV byte slice without string allocations.
// Uses fast bytes.IndexByte scanning and zero-alloc numeric parsing.
func parseTickLineBytes(line []byte, defaultSymbol string) (model.Tick, error) {
	// Detect delimiter (comma or tab)
	delim := byte(',')
	if bytes.IndexByte(line, '\t') != -1 {
		delim = '\t'
	}

	// Extract tokens by slicing byte slices directly (0 heap allocations)
	var tokens [5][]byte
	numTokens := 0
	start := 0

	for i := 0; i < len(line) && numTokens < 5; i++ {
		if line[i] == delim {
			tokens[numTokens] = bytes.TrimSpace(line[start:i])
			numTokens++
			start = i + 1
		}
	}
	if start < len(line) && numTokens < 5 {
		tokens[numTokens] = bytes.TrimSpace(line[start:])
		numTokens++
	}

	if numTokens < 2 {
		return model.Tick{}, fmt.Errorf("insufficient fields")
	}

	// Format 1: TimestampNs, Bid, Ask (all numeric)
	if numTokens >= 3 {
		ts, errTs := parseIntBytes(tokens[0])
		if errTs == nil && ts > 1e12 {
			bid, err1 := parseFloatBytes(tokens[1])
			ask, err2 := parseFloatBytes(tokens[2])
			if err1 == nil && err2 == nil {
				return model.Tick{
					Symbol:      defaultSymbol,
					Bid:         bid,
					Ask:         ask,
					TimestampNs: ts,
				}, nil
			}
		}
	}

	// Format 2: DateTime string, Bid, Ask (MT5 format: "YYYY.MM.DD HH:MM:SS.mmm,Bid,Ask,...")
	if numTokens >= 3 {
		bid, err1 := parseFloatBytes(tokens[1])
		ask, err2 := parseFloatBytes(tokens[2])
		if err1 == nil && err2 == nil {
			ts := parseDateTimeBytes(tokens[0])
			return model.Tick{
				Symbol:      defaultSymbol,
				Bid:         bid,
				Ask:         ask,
				TimestampNs: ts,
			}, nil
		}
	}

	// Format 3: Date, Time, Bid, Ask (4 columns: "YYYY-MM-DD,HH:MM:SS,Bid,Ask")
	if numTokens >= 4 && numTokens < 5 {
		bid, err1 := parseFloatBytes(tokens[2])
		ask, err2 := parseFloatBytes(tokens[3])
		if err1 == nil && err2 == nil {
			ts := parseCombinedDateTimeBytes(tokens[0], tokens[1])
			return model.Tick{
				Symbol:      defaultSymbol,
				Bid:         bid,
				Ask:         ask,
				TimestampNs: ts,
			}, nil
		}
	}

	// Format 4: Exness Official Tick Export (5 columns: "Broker","Symbol","Timestamp","Bid","Ask")
	if numTokens >= 5 {
		bidB := bytes.Trim(tokens[3], "\"")
		askB := bytes.Trim(tokens[4], "\"")
		bid, err1 := parseFloatBytes(bidB)
		ask, err2 := parseFloatBytes(askB)
		if err1 == nil && err2 == nil {
			tsB := bytes.Trim(tokens[2], "\"")
			ts := parseDateTimeBytes(tsB)
			symB := bytes.Trim(tokens[1], "\"")
			sym := string(symB)
			if sym == "" {
				sym = defaultSymbol
			}
			return model.Tick{
				Symbol:      sym,
				Bid:         bid,
				Ask:         ask,
				TimestampNs: ts,
			}, nil
		}
	}

	return model.Tick{}, fmt.Errorf("unrecognized line format")
}

// parseFloatBytes parses ASCII decimal bytes (e.g. "1.15704" or "-0.05") without string allocation.
func parseFloatBytes(b []byte) (float64, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("empty byte slice")
	}

	neg := false
	idx := 0
	if b[0] == '-' {
		neg = true
		idx = 1
	} else if b[0] == '+' {
		idx = 1
	}

	var integerPart float64
	var fracPart float64
	var fracDiv float64 = 1.0
	inFrac := false
	digitsParsed := 0

	for i := idx; i < len(b); i++ {
		c := b[i]
		if c >= '0' && c <= '9' {
			digitsParsed++
			digit := float64(c - '0')
			if inFrac {
				fracDiv *= 10.0
				fracPart = fracPart*10.0 + digit
			} else {
				integerPart = integerPart*10.0 + digit
			}
		} else if c == '.' && !inFrac {
			inFrac = true
		} else {
			break
		}
	}

	if digitsParsed == 0 {
		return 0, fmt.Errorf("no valid digits parsed")
	}

	val := integerPart + (fracPart / fracDiv)
	if neg {
		val = -val
	}
	return val, nil
}

// parseIntBytes parses ASCII integer bytes into int64 without string allocation.
func parseIntBytes(b []byte) (int64, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("empty byte slice")
	}

	neg := false
	idx := 0
	if b[0] == '-' {
		neg = true
		idx = 1
	} else if b[0] == '+' {
		idx = 1
	}

	var res int64
	for i := idx; i < len(b); i++ {
		c := b[i]
		if c >= '0' && c <= '9' {
			res = res*10 + int64(c-'0')
		} else {
			break
		}
	}

	if neg {
		res = -res
	}
	return res, nil
}

// parseDateTimeBytes converts MT5 timestamp format byte slices to Unix nanoseconds.
func parseDateTimeBytes(b []byte) int64 {
	// Expected format: "2026.08.14 12:00:00.123" (23 bytes) or "2026.08.14 12:00:00" (19 bytes)
	if len(b) < 19 {
		return time.Now().UnixNano()
	}

	year, _ := parseIntBytes(b[0:4])
	month, _ := parseIntBytes(b[5:7])
	day, _ := parseIntBytes(b[8:10])
	hour, _ := parseIntBytes(b[11:13])
	min, _ := parseIntBytes(b[14:16])
	sec, _ := parseIntBytes(b[17:19])

	ms := int64(0)
	if len(b) >= 23 && b[19] == '.' {
		ms, _ = parseIntBytes(b[20:23])
	}

	t := time.Date(int(year), time.Month(month), int(day), int(hour), int(min), int(sec), int(ms*1e6), time.UTC)
	return t.UnixNano()
}

// parseCombinedDateTimeBytes parses separate date ("YYYY-MM-DD" or "YYYY.MM.DD") and time ("HH:MM:SS") bytes.
func parseCombinedDateTimeBytes(dateB, timeB []byte) int64 {
	if len(dateB) < 10 || len(timeB) < 8 {
		return time.Now().UnixNano()
	}

	year, _ := parseIntBytes(dateB[0:4])
	month, _ := parseIntBytes(dateB[5:7])
	day, _ := parseIntBytes(dateB[8:10])
	hour, _ := parseIntBytes(timeB[0:2])
	min, _ := parseIntBytes(timeB[3:5])
	sec, _ := parseIntBytes(timeB[6:8])

	t := time.Date(int(year), time.Month(month), int(day), int(hour), int(min), int(sec), 0, time.UTC)
	return t.UnixNano()
}
