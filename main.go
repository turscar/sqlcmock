package main

import (
	_ "embed"
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"go.turscar.ie/sqlcmock/generator"
	"log"
	"os"
)

func main() {
	err := run()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("")
}

func run() error {
	opts := generator.Opts{}
	flag.StringVar(&opts.TemplateFile, "template", "", "Template File")
	flag.StringVar(&opts.OutputFile, "output", "", "Output File")
	flag.StringVar(&opts.OutputPackage, "package", "", "Output Package")
	flag.BoolVar(&opts.Format, "format", true, "Format output")
	flag.Parse()
	if flag.NArg() == 0 {
		return errors.New("usage: shmock path/to/querier.go")
	}

	opts.InputFile = flag.Arg(0)

	output, err := generator.Parse(opts)
	if err != nil {
		return err
	}
	generated, err := output.Render(opts)
	if err != nil {
		return err
	}

	err = os.WriteFile(opts.OutputFile, generated, 0644)
	if err != nil {
		return err
	}
	return nil
}
