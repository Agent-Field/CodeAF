package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

func factoryPipelineRecipe(dir string) (factory.Recipe, []factory.Problem, error) {
	return pipeline.RecipeLoader(home.Join("factory.yaml"))(dir)
}

func factoryPipelineOption() factory.LocalOption {
	return factory.WithRecipeLoader(factoryPipelineRecipe)
}

func runFactoryConvert(args []string, out, warnings io.Writer) error {
	flags := commandFlags("factory convert")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return wrongCall("codeaf factory convert takes one Markdown file")
	}
	path := filepath.FromSlash(factory.RecipeFile)
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data, problems, err := pipeline.Convert(string(text))
	if err != nil {
		return err
	}
	for _, problem := range problems {
		if _, err := fmt.Fprintln(warnings, problem.Error()); err != nil {
			return err
		}
	}
	_, err = out.Write(data)
	return err
}

// factoryIntake reuses the same runner door as a person's Run action, including
// the mailbox when another process owns execution on this machine.
func factoryIntake(st *store.Store) *intake.Intake {
	history, _ := record.Open(st, record.Options{})
	return intake.New(st, intake.Options{
		Record: history,
		Load: func(repo string) (pipeline.File, error) {
			dir := st.CheckoutDir(repo)
			if dir == "" {
				return pipeline.Defaults(), nil
			}
			return pipeline.LoadMerged(dir, home.Join("factory.yaml"))
		},
		Launch: func(id int) error {
			seam := factory.LocalSeam(st, time.Now(), factoryRunnerDoors(st), factoryPipelineOption(), factory.WithRepoDirs(st.CheckoutDir))
			if seam.Launch == nil {
				return errors.New("factory has no worker to start this job")
			}
			return seam.Launch(id)
		},
	})
}
