package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/rtcbench/rtcbench/internal/bench"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "analyze" {
		return analyze(args[1:], stdout)
	}
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	flags := flag.NewFlagSet("bench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	mode := flags.String("mode", "receiver", "receiver or sender capacity")
	sfus := flags.String("sfus", "janus,jitsi,livekit,mediasoup", "comma-separated SFUs")
	clients := flags.String("clients", "rtcbench,webrtcperf,chromium", "comma-separated clients")
	cells := flags.String("cells", "", "specific combinations: livekit:rtcbench,chromium;janus:rtcbench")
	minimum := flags.Int("min-r", 1, "minimum participants per load machine")
	maximum := flags.Int("max-r", 5000, "maximum participants per load machine")
	repetitions := flags.Int("repetitions", 2, "repetitions required at every load")
	dryRun := flags.Bool("dry-run", false, "simulate search without contacting machines")
	quick := flags.Bool("quick", false, "short development timings")
	provision := flags.Bool("provision", false, "start/stop SFUs using the deployment helpers")
	upload := flags.Bool("upload-assets", false, "upload prepared binaries and media before provisioning")
	output := flags.String("out", "", "result directory")
	hintsFile := flags.String("hints", "hints.json", "previous capacities used only as a first probe")
	ignoreHints := flags.Bool("ignore-hints", false, "do not read or update hints")
	_ = flags.Bool("verbose", false, "show progress")
	_ = flags.Bool("v", false, "show progress")
	clusterFile := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		clusterFile = args[0]
		args = args[1:]
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if clusterFile == "" && flags.NArg() == 1 {
		clusterFile = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return fmt.Errorf("expected one cluster YAML file")
	}
	if clusterFile == "" {
		return fmt.Errorf("usage: bench [run] [flags] cluster.yml; bench analyze results-directory")
	}
	data, err := os.ReadFile(clusterFile)
	if err != nil {
		return err
	}
	if *quick {
		c, err := bench.ParseCluster(string(data))
		if err != nil {
			return err
		}
		c.WarmupSeconds, c.DurationSeconds, c.StartLeadSeconds, c.TeardownSeconds = 10, 20, 10, 3
		data, err = yaml.Marshal(c)
		if err != nil {
			return err
		}
		if *maximum == 5000 {
			*maximum = 15
		}
	}
	selection := *cells
	if selection == "" {
		var groups []string
		for _, sfu := range strings.Split(*sfus, ",") {
			groups = append(groups, strings.TrimSpace(sfu)+":"+*clients)
		}
		selection = strings.Join(groups, ";")
	}
	combinations, err := bench.ParseCells(selection)
	if err != nil {
		return err
	}
	request := bench.Request{
		Name:         "Capacity",
		ClusterYAML:  string(data),
		Mode:         *mode,
		Cells:        combinations,
		Bounds:       bench.Bounds{Min: *minimum, Max: *maximum},
		Repetitions:  *repetitions,
		Provision:    *provision,
		UploadAssets: *upload,
		DryRun:       *dryRun,
	}
	if !*ignoreHints {
		request.Hints, err = bench.LoadHints(*hintsFile, *mode)
		if err != nil {
			return err
		}
	}
	if *output == "" {
		*output = filepath.Join("results", time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z"))
	}
	report, err := bench.Run(ctx, request, bench.Options{Directory: *output, Log: stderr})
	if err == nil && !*ignoreHints && !*dryRun && *minimum != *maximum {
		err = bench.SaveHints(*hintsFile, *mode, report)
	}
	if *dryRun {
		fmt.Fprintln(stdout, "SIMULATION: these are not measured capacities.")
	}
	for _, cell := range combinations {
		result, ok := report.Results[cell.Key()]
		if !ok {
			continue
		}
		if !result.Complete {
			fmt.Fprintf(stdout, "%s: incomplete (highest verified load %d)\n", cell.Key(), result.Best)
			continue
		}
		mark := ""
		if result.CeilingReached {
			mark = ">="
		}
		fmt.Fprintf(stdout, "%s: %s%d %ss per machine\n", cell.Key(), mark, result.Best, *mode)
	}
	fmt.Fprintln(stdout, "Results:", *output)
	return err
}

func analyze(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bench analyze results-directory-or-jsonl")
	}
	path := args[0]
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		path = filepath.Join(path, "experiments.jsonl")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	analysis, err := bench.Analyze(file)
	if err != nil {
		return err
	}
	return analysis.Print(out)
}
