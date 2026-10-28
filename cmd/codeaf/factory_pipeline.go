package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/home"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// factoryPipelineOption is the seam's recipes: a repository's is main's
// file, or the factory's own copy in its place when st keeps one
// ([factoryRepoRead]); a job's is the copy it carries first
// ([factoryJobFile]). st is nil for a worker's mirror, which keeps no copy:
// the jobs it runs carry theirs.
func factoryPipelineOption(st *store.Store) factory.LocalOption {
	return factory.WithOptions(pipeline.LocalFiles(),
		factory.WithRepositoryRecipeLoader(func(repo, dir string) (factory.Recipe, []factory.Problem, error) {
			f, err := factoryRepoRead(context.Background(), st, repo, dir)
			return f.Recipe(), f.RecipeProblems(), err
		}),
		factory.WithJobRecipeLoader(func(it factory.Item, dir string) (factory.Recipe, []factory.Problem, error) {
			f, err := factoryJobFile(context.Background(), st, it, it.Repo, dir)
			return f.Recipe(), f.RecipeProblems(), err
		}))
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

// factoryIntake reuses the same runner door as a person's Run action: the
// lease's, which queues the job for whichever worker pulls it.
func factoryIntake(st *store.Store) *intake.Intake {
	return factoryIntakeOwned(st, factoryProfileOwner)
}

// factoryIntakeOwned is [factoryIntake] whose rule-started jobs belong to
// owner: the factory's operator, as the host that runs the intake knows them.
func factoryIntakeOwned(st *store.Store, owner func(context.Context) string) *intake.Intake {
	history, _ := record.Open(st, record.Options{})
	return intake.New(st, intake.Options{
		Owner:  owner,
		Record: history,
		Refresh: func(ctx context.Context) error {
			repos, err := st.Repos()
			if err != nil {
				return err
			}
			for _, repo := range repos {
				factoryRules().Refresh(ctx, repo, st.CheckoutDir(repo))
				factoryPeople().RefreshOwners(ctx, repo)
			}
			return ctx.Err()
		},
		Load: func(repo string) (pipeline.File, error) {
			return factoryPipelineRead(context.Background(), st, repo, st.CheckoutDir(repo))
		},
		Launch: func(id int) error {
			seam := factory.LocalSeam(st, time.Now(), factoryRunnerDoors(st), factoryPipelineOption(st), factory.WithRepoDirs(st.CheckoutDir))
			if seam.Launch == nil {
				return errors.New("factory has no worker to start this job")
			}
			return seam.Launch(id)
		},
	})
}

// Identity and ownership reads share one loader per profile with the poll.
var factoryPeopleCaches = struct {
	sync.Mutex
	profiles map[string]*factorygithub.PeopleLoader
}{profiles: map[string]*factorygithub.PeopleLoader{}}

func factoryPeople() *factorygithub.PeopleLoader {
	profile := factoryProfile("")
	factoryPeopleCaches.Lock()
	defer factoryPeopleCaches.Unlock()
	if loader := factoryPeopleCaches.profiles[profile]; loader != nil {
		return loader
	}
	loader := factorygithub.NewPeopleLoader(func(ctx context.Context) factorygithub.PeopleAPI {
		token, _ := factorygithub.TokenAt(ctx, profile)
		return forge.NewClient(token)
	}, nil)
	factoryPeopleCaches.profiles[profile] = loader
	return loader
}

// Every factory door in this process shares the same repository and blob cache.
// Profiles have separate loaders so changing a home cannot reuse another token's read.
var factoryRuleCaches = struct {
	sync.Mutex
	profiles map[string]*factorygithub.PipelineLoader
}{profiles: map[string]*factorygithub.PipelineLoader{}}

func factoryRules() *factorygithub.PipelineLoader {
	profile := factoryProfile("")
	factoryRuleCaches.Lock()
	defer factoryRuleCaches.Unlock()
	if loader := factoryRuleCaches.profiles[profile]; loader != nil {
		return loader
	}
	loader := factorygithub.NewPipelineLoader(func(ctx context.Context) factorygithub.PipelineAPI {
		token, _ := factorygithub.TokenAt(ctx, profile)
		if token == "" {
			return nil
		}
		return forge.NewClient(token)
	})
	factoryRuleCaches.profiles[profile] = loader
	return loader
}

// factoryPipelineRead is one repository's pipeline as THIS FACTORY runs it,
// for every reader that decides or shows what a job runs under: the main
// branch's file (forge first, the checkout's main branch when offline) with the
// personal file, then THE FACTORY'S OWN COPY in its place when the store keeps
// one ([pipeline.WithFactoryCopy]: it clears itself once main says the same),
// then the factory's own rules on top ([withFactoryHouseRules]). The order is
// the precedence factory.RecipeSource records: copy, else main, else built-in.
func factoryPipelineRead(ctx context.Context, st *store.Store, repo, dir string) (pipeline.File, error) {
	f, err := factoryRepoRead(ctx, st, repo, dir)
	return withFactoryHouseRules(st, f, err)
}

// factoryJobRead is [factoryPipelineRead] for ONE JOB: the factory's copy the
// job carries in place of the repository's read ([factoryJobFile]), with the
// factory's own rules on top. Every door that edits a job's steps reads this.
func factoryJobRead(ctx context.Context, st *store.Store, it factory.Item, dir string) (pipeline.File, error) {
	f, err := factoryJobFile(ctx, st, it, factory.RepoOf(it), dir)
	return withFactoryHouseRules(st, f, err)
}

// factoryRepoRead is one repository's pipeline before the factory's own
// rules: main's file with the personal file, or the factory's copy in its
// place when st keeps one ([pipeline.WithFactoryCopy], which is handed main's
// read before the personal file and merges it itself). A nil st keeps no
// copy.
func factoryRepoRead(ctx context.Context, st *store.Store, repo, dir string) (pipeline.File, error) {
	personal := home.Join("factory.yaml")
	if st == nil {
		return factoryRules().Load(ctx, repo, dir, personal)
	}
	f, err := factoryRules().Load(ctx, repo, dir, "")
	return pipeline.WithFactoryCopy(f, err, st, repo, personal)
}

// factoryJobFile is THE ONE RESOLVER of the file a job is bounded by, before
// the factory's own rules ([pipeline.JobFile]): THE COPY THE JOB CARRIES
// ([factory.Item.PipelineCopy]) with the personal file, else this factory's
// read of repo now ([factoryRepoRead]). The edit doors, the manager's brief,
// the item card and the runner (its rules, its plan's bounds and a post's
// policy) all read it, so none of them bounds a job by a file it does not run.
func factoryJobFile(ctx context.Context, st *store.Store, it factory.Item, repo, dir string) (pipeline.File, error) {
	return pipeline.JobFile(it.PipelineCopy, home.Join("factory.yaml"), func() (pipeline.File, error) {
		return factoryRepoRead(ctx, st, repo, dir)
	})
}

// withFactoryHouseRules adds the factory's own rules (the store's, see
// [factory.HouseRules]) to one repository's read, for every reader that
// decides or shows what a job runs under: intake's start decision and the
// factory's pipeline answers. A store that cannot read its rules makes the
// read fail rather than quietly decide without them, so nothing starts on
// its own that a house rule would have held.
func withFactoryHouseRules(st *store.Store, f pipeline.File, err error) (pipeline.File, error) {
	if st == nil {
		return f, err
	}
	house, herr := st.HouseRules()
	if herr != nil {
		return f, fmt.Errorf("%w: %v", pipeline.ErrHouseRulesUnread, herr)
	}
	return pipeline.WithHouseRules(f, house), err
}

// factoryProfileOwner is the forge login of this machine's GitHub connection:
// on a factory a person serves from their own machine, they are its operator.
// "" when there is no connection or GitHub cannot say.
func factoryProfileOwner(ctx context.Context) string {
	token, _ := factorygithub.TokenAt(ctx, factoryProfile(""))
	if token == "" {
		return ""
	}
	login, err := forge.NewClient(token).Me(ctx)
	if err != nil {
		return ""
	}
	return login
}

// factoryOperatorOwner is a team server's operator, from its persisted claim
// beside the tokens file; "" before anyone has claimed it.
func factoryOperatorOwner(tokensPath string) func(context.Context) string {
	return func(context.Context) string {
		claim, err := tokens.LoadOperator(tokensPath)
		if err != nil {
			return ""
		}
		return claim.Login
	}
}
