package delete

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmd/run/shared"
	workflowShared "github.com/cli/cli/v2/pkg/cmd/workflow/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/cli/cli/v2/pkg/prompt"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
)

func TestNewCmdDelete(t *testing.T) {
	tests := []struct {
		name     string
		cli      string
		tty      bool
		wants    DeleteOptions
		wantsErr bool
	}{
		{
			name: "blank tty",
			tty:  true,
			wants: DeleteOptions{
				Prompt: true,
			},
		},
		{
			name:     "blank nontty",
			wantsErr: true,
		},
		{
			name: "with arg",
			cli:  "1234",
			wants: DeleteOptions{
				RunID: "1234",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, _, _ := iostreams.Test()
			ios.SetStdinTTY(tt.tty)
			ios.SetStdoutTTY(tt.tty)

			f := &cmdutil.Factory{
				IOStreams: ios,
			}

			argv, err := shlex.Split(tt.cli)
			assert.NoError(t, err)

			var gotOpts *DeleteOptions
			cmd := NewCmdDelete(f, func(opts *DeleteOptions) error {
				gotOpts = opts
				return nil
			})

			cmd.SetArgs(argv)
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			_, err = cmd.ExecuteC()
			if tt.wantsErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			assert.Equal(t, tt.wants.RunID, gotOpts.RunID)
		})
	}
}

func TestRunDelete(t *testing.T) {
	inProgressRun := shared.TestRun(1234, shared.InProgress, "")
	completedRun := shared.TestRun(4567, shared.Completed, shared.Failure)
	tests := []struct {
		name      string
		httpStubs func(*httpmock.Registry)
		askStubs  func(*prompt.AskStubber)
		opts      *DeleteOptions
		wantErr   bool
		wantOut   string
		errMsg    string
	}{
		{
			name: "delete run",
			opts: &DeleteOptions{
				RunID: "1234",
			},
			wantErr: false,
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/runs/1234"),
					httpmock.JSONResponse(inProgressRun))
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/workflows/123"),
					httpmock.JSONResponse(shared.TestWorkflow))
				reg.Register(
					httpmock.REST("POST", "repos/OWNER/REPO/actions/runs/1234/delete"),
					httpmock.StatusStringResponse(202, "{}"))
			},
			wantOut: "✓ Request to delete workflow submitted.\n",
		},
		{
			name: "not found",
			opts: &DeleteOptions{
				RunID: "1234",
			},
			wantErr: true,
			errMsg:  "Could not find any workflow run with ID 1234",
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/runs/1234"),
					httpmock.StatusStringResponse(404, ""))
			},
		},
		{
			name: "completed",
			opts: &DeleteOptions{
				RunID: "4567",
			},
			wantErr: true,
			errMsg:  "Cannot delete a workflow run that hasn't completed",
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/runs/4567"),
					httpmock.JSONResponse(completedRun))
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/workflows/123"),
					httpmock.JSONResponse(shared.TestWorkflow))
				reg.Register(
					httpmock.REST("POST", "repos/OWNER/REPO/actions/runs/4567/delete"),
					httpmock.StatusStringResponse(409, ""),
				)
			},
		},
		{
			name: "prompt, no in progress runs",
			opts: &DeleteOptions{
				Prompt: true,
			},
			wantErr: true,
			errMsg:  "found no completed runs to delete",
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/runs"),
					httpmock.JSONResponse(shared.RunsPayload{
						WorkflowRuns: []shared.Run{
							completedRun,
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/workflows"),
					httpmock.JSONResponse(workflowShared.WorkflowsPayload{
						Workflows: []workflowShared.Workflow{
							shared.TestWorkflow,
						},
					}))
			},
		},
		{
			name: "prompt, delete",
			opts: &DeleteOptions{
				Prompt: true,
			},
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/runs"),
					httpmock.JSONResponse(shared.RunsPayload{
						WorkflowRuns: []shared.Run{
							inProgressRun,
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/workflows"),
					httpmock.JSONResponse(workflowShared.WorkflowsPayload{
						Workflows: []workflowShared.Workflow{
							shared.TestWorkflow,
						},
					}))
				reg.Register(
					httpmock.REST("POST", "repos/OWNER/REPO/actions/runs/1234/delete"),
					httpmock.StatusStringResponse(202, "{}"))
			},
			askStubs: func(as *prompt.AskStubber) {
				//nolint:staticcheck // SA1019: as.StubOne is deprecated: use StubPrompt
				as.StubOne(0)
			},
			wantOut: "✓ Request to delete workflow submitted.\n",
		},
	}

	for _, tt := range tests {
		reg := &httpmock.Registry{}
		tt.httpStubs(reg)
		tt.opts.HttpClient = func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		}

		ios, _, stdout, _ := iostreams.Test()
		ios.SetStdoutTTY(true)
		ios.SetStdinTTY(true)
		tt.opts.IO = ios
		tt.opts.BaseRepo = func() (ghrepo.Interface, error) {
			return ghrepo.FromFullName("OWNER/REPO")
		}

		//nolint:staticcheck // SA1019: prompt.InitAskStubber is deprecated: use NewAskStubber
		as, teardown := prompt.InitAskStubber()
		defer teardown()
		if tt.askStubs != nil {
			tt.askStubs(as)
		}

		t.Run(tt.name, func(t *testing.T) {
			err := runDelete(tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Equal(t, tt.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantOut, stdout.String())
			reg.Verify(t)
		})
	}
}
