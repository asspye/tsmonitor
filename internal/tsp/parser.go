package tsp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Префиксы, с которыми плагины пишут JSON (--json-line=PREFIX)
const (
	AnalyzePrefix = "AN:"
	SCTEPrefix    = "SCTE:"
)

var (
	iatRegex       = regexp.MustCompile(`IAT: ([\d,]+) microseconds \(std\.dev: ([\d,]+), min: ([\d,]+), max: ([\d,]+)\)`)
	pcrJitterRegex = regexp.MustCompile(`pcrverify: PID 0x[0-9A-Fa-f]+ \((\d+)\), PCR jitter: -?[\d,]+ = (-?[\d,]+) micro-seconds`)
)

// LineKind — тип строки вывода tsp
type LineKind int

const (
	LineOther LineKind = iota
	LineAnalyze
	LineIAT
	LinePCRJitter
	LineSCTE
)

// Classify определяет тип строки и возвращает полезную нагрузку (JSON без префикса)
func Classify(line string) (LineKind, string) {
	if i := strings.Index(line, AnalyzePrefix+"{"); i >= 0 {
		return LineAnalyze, line[i+len(AnalyzePrefix):]
	}
	if i := strings.Index(line, SCTEPrefix+"{"); i >= 0 {
		return LineSCTE, line[i+len(SCTEPrefix):]
	}
	if strings.Contains(line, "iat: IAT:") {
		return LineIAT, line
	}
	if strings.Contains(line, "pcrverify: PID") {
		return LinePCRJitter, line
	}
	return LineOther, line
}

// ParseAnalyze разбирает JSON отчёта analyze
func ParseAnalyze(payload string) (*AnalyzeReport, error) {
	var r AnalyzeReport
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		return nil, fmt.Errorf("analyze json: %w", err)
	}
	return &r, nil
}

// ParseIAT разбирает строку iat
func ParseIAT(line string) (*IATReport, error) {
	m := iatRegex.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("not an iat line")
	}
	v := make([]int64, 4)
	for i := range v {
		n, err := parseNum(m[i+1])
		if err != nil {
			return nil, err
		}
		v[i] = n
	}
	return &IATReport{MeanUS: v[0], StdDevUS: v[1], MinUS: v[2], MaxUS: v[3]}, nil
}

// ParsePCRJitter разбирает строку pcrverify о превышении порога
func ParsePCRJitter(line string) (*PCRJitter, error) {
	m := pcrJitterRegex.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("not a pcr jitter line")
	}
	pid, _ := strconv.Atoi(m[1])
	us, err := parseNum(m[2])
	if err != nil {
		return nil, err
	}
	if us < 0 {
		us = -us
	}
	return &PCRJitter{PID: pid, JitterUS: us}, nil
}

func parseNum(s string) (int64, error) {
	return strconv.ParseInt(strings.ReplaceAll(s, ",", ""), 10, 64)
}
