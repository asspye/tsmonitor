package tsp

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Options — параметры запуска tsp для одного потока
type Options struct {
	LocalAddress  string        // адрес интерфейса для multicast
	URL           string        // group:port или HLS-плейлист
	Interval      time.Duration // период отчётов analyze/iat
	ReceiveBuffer int           // --buffer-size для -I ip, 0 — по умолчанию
	PCRJitterMax  time.Duration // порог pcrverify, 0 — без pcrverify
	SCTE35        bool          // запускать splicemonitor
}

// IsHLS — поток задан HLS-плейлистом, а не multicast-адресом
func IsHLS(url string) bool {
	return strings.Contains(url, "://") || strings.HasSuffix(strings.ToLower(url), ".m3u8")
}

// BuildArgs собирает аргументы tsp.
// iat и pcrverify -i имеют смысл только для датаграмм с временем приёма ядра,
// поэтому для HLS не запускаются.
func BuildArgs(o Options) []string {
	secs := strconv.Itoa(int(o.Interval / time.Second))
	var args []string

	hls := IsHLS(o.URL)
	if hls {
		args = append(args, "-I", "hls", "--live", o.URL)
	} else {
		args = append(args, "-I", "ip", "--local-address", o.LocalAddress)
		if o.ReceiveBuffer > 0 {
			args = append(args, "--buffer-size", strconv.Itoa(o.ReceiveBuffer))
		}
		args = append(args, o.URL)
		args = append(args, "-P", "iat", "--interval", secs)
		if o.PCRJitterMax > 0 {
			args = append(args, "-P", "pcrverify", "--input-synchronous",
				"--jitter-max", strconv.FormatInt(o.PCRJitterMax.Microseconds(), 10))
		}
	}
	if o.SCTE35 {
		args = append(args, "-P", "splicemonitor", "--all-commands", "--json-line="+SCTEPrefix)
	}
	args = append(args,
		"-P", "analyze", "--interval", secs, "--json-line="+AnalyzePrefix,
		"-O", "drop",
	)
	return args
}

// Runner держит процесс tsp запущенным и передаёт каждую строку его вывода в OnLine
type Runner struct {
	Opts         Options
	OnLine       func(line string)
	OnExit       func(err error) // вызывается после каждого завершения tsp
	RestartDelay time.Duration
	StopTimeout  time.Duration
}

// maxLine — предел длины строки: JSON analyze для MPTS с десятками PID бывает большим
const maxLine = 4 * 1024 * 1024

// Run запускает tsp и перезапускает его после выхода, пока не отменён ctx
func (r *Runner) Run(ctx context.Context) {
	delay := r.RestartDelay
	if delay == 0 {
		delay = 5 * time.Second
	}
	for {
		err := r.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if r.OnExit != nil {
			r.OnExit(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	defer pr.Close()

	cmd := exec.Command("tsp", BuildArgs(r.Opts)...)
	cmd.Stdout = pw
	cmd.Stderr = pw
	setProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		pw.Close()
		return fmt.Errorf("start tsp: %w", err)
	}
	pw.Close() // пишущий конец остаётся только у tsp: EOF придёт, когда он завершится

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			cmd.Process.Signal(os.Interrupt)
			stop := r.StopTimeout
			if stop == 0 {
				stop = 3 * time.Second
			}
			select {
			case <-done:
			case <-time.After(stop):
				cmd.Process.Kill()
			}
		case <-done:
		}
	}()

	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		if r.OnLine != nil {
			r.OnLine(sc.Text())
		}
	}
	scanErr := sc.Err()
	if scanErr != nil {
		// Слишком длинная строка: дальше читать нельзя — останавливаем tsp
		cmd.Process.Kill()
	}

	waitErr := cmd.Wait()
	if scanErr != nil {
		return fmt.Errorf("read tsp output: %w", scanErr)
	}
	if waitErr != nil {
		return fmt.Errorf("tsp exited: %w", waitErr)
	}
	return fmt.Errorf("tsp exited")
}
