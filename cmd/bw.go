package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
)

const (
	nextStep = 1024
)

type (
	unit struct {
		count float64
		desc  string
	}
)

var (
	descs = []string{
		"B",
		"KB",
		"MB",
		"GB",
		"TB",
		"PB",
	}
)

func main() {
	var (
		socket string
		port   int
		src    io.Reader
		mb     int
	)

	app := &cli.App{
		Name:  "bw",
		Usage: "Measure data bandwidth through a socket or port.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "file",
				Aliases:     []string{"f"},
				Usage:       "file to read from",
				Destination: &socket,
			},
			&cli.StringFlag{
				Name:        "socket",
				Aliases:     []string{"s"},
				Usage:       "unix socket to read from",
				Destination: &socket,
			},
			&cli.IntFlag{
				Name:        "port",
				Aliases:     []string{"p"},
				Usage:       "port to read data from",
				Destination: &port,
			},
			&cli.IntFlag{
				Name:        "mb",
				Aliases:     []string{"m"},
				Usage:       "read up to this many mb at a time",
				Destination: &mb,
				DefaultText: "1",
			},
		},
		Action: func(c *cli.Context) error {
			// validate the filetype
			if port == 0 && socket == "" && !isStdin() {
				return fmt.Errorf("must provide at least unix socket, port, or be writing to stdin")
			}
			if port != 0 && socket != "" {
				return fmt.Errorf("must only specify either a port, socket, or be writing to stdin")
			}

			if isStdin() {
				src = os.Stdin
			}

			if mb == 0 {
				mb = 1
			}

			return nil
		},
		Authors: []*cli.Author{
			&cli.Author{
				Name:  "Michael Fitz-Payne",
				Email: "michael@michaelfp.com",
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// channel for sending bytes read to the counter
	readCounter := make(chan int64)
	defer close(readCounter)

	// context passed into reader and calculater
	ctx, cancel := context.WithCancel(context.Background())

	// catch sigint so we can shutdown gracefully
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		cancel()
	}()

	// run the main read and count loops
	go CalculateBandwidth(ctx, readCounter)
	if err := ReadData(ctx, readCounter, src, mb); err != nil {
		<-ctx.Done()
	}

	os.Exit(0)
	return
}

// isStdin returns true when file has data piped from stdin.
func isStdin() bool {
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// ReadData reads from the pipe.Reader(), sending the amount of bytes read to bRead,
// and writing the bytes to the pipe.Writer(). If no data can be read, the function
// will exit and cxt will be cancelled.
func ReadData(ctx context.Context, bRead chan int64, src io.Reader, r int) error {

	// ensure the reader is buffered for performance
	bufSrc := bufio.NewReader(src)
	bin := bufio.NewWriter(ioutil.Discard)
	rw := bufio.NewReadWriter(bufSrc, bin)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			bufSize := nextStep * nextStep * int64(r)
			bufSize = 409500
			n, err := io.CopyN(rw, rw, bufSize)
			if err != nil {
				return err
			}
			bRead <- int64(n)
		}
	}

	return nil
}

// CalculateBandwidth keeps track of the amount of bytes read, and calculates a
// per-second average as well as the average across the whole runtime of the program.
func CalculateBandwidth(ctx context.Context, bRead chan int64) {
	// no mutex required for these as only one of the select cases below
	// can be running at a time
	var total int64
	var prevSecond int64
	start := time.Now()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case n := <-bRead:
				total += n
				prevSecond += n
			case _ = <-ticker.C:
				elapsed := time.Since(start)
				prevDesc := getUnits(prevSecond)
				totalDesc := getUnits(total)
				avg := totalDesc.count / elapsed.Seconds()
				fmt.Printf("\rcurrent: %.0f %v/s\t", prevDesc.count, prevDesc.desc)
				fmt.Printf("average: %.4f %v/s", avg, totalDesc.desc)
				prevSecond = 0
			}
		}
	}()

	<-ctx.Done()
	elapsed := time.Since(start)
	totalDesc := getUnits(total)
	fmt.Printf("\ntotal bytes read in %.2f seconds: %.0f %v\n",
		elapsed.Seconds(),
		totalDesc.count,
		totalDesc.desc)
	return
}

func reducer(remCount float64, descIdx int) unit {
	if remCount < float64(nextStep) {
		// Not checking for descIdx < descs here because anything
		// greater than PB/s is _really_ unlikely.
		return unit{
			count: remCount,
			desc:  descs[descIdx],
		}
	}

	descIdx += 1
	return reducer(remCount/nextStep, descIdx)
}

// getUnits returns the byte count in a human readable format based on the appropriate
// level of accuracy.
// e.g.
// if 2048 bytes are read, that will be returned as 2 KB
// if 2096 KB are read, that will be returned as 2 MB
// and so on.
func getUnits(count int64) unit {
	return reducer(float64(count), 0)
}
