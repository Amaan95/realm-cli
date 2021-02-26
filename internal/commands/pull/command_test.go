package pull

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/10gen/realm-cli/internal/cli"
	"github.com/10gen/realm-cli/internal/cloud/realm"
	"github.com/10gen/realm-cli/internal/local"
	u "github.com/10gen/realm-cli/internal/utils/test"
	"github.com/10gen/realm-cli/internal/utils/test/assert"
	"github.com/10gen/realm-cli/internal/utils/test/mock"
)

func TestPullHandler(t *testing.T) {
	t.Run("should return an error if the command fails to resolve from", func(t *testing.T) {
		var realmClient mock.RealmClient
		realmClient.FindAppsFn = func(filter realm.AppFilter) ([]realm.App, error) {
			return nil, errors.New("something bad happened")
		}

		cmd := &Command{inputs{From: "somewhere"}}

		err := cmd.Handler(nil, nil, cli.Clients{Realm: realmClient})
		assert.Equal(t, errors.New("something bad happened"), err)
	})

	t.Run("should return an error if the command fails to do the export", func(t *testing.T) {
		_, ui := mock.NewUI()

		var realmClient mock.RealmClient
		realmClient.FindAppsFn = func(filter realm.AppFilter) ([]realm.App, error) {
			return nil, nil
		}
		realmClient.ExportFn = func(groupID, appID string, req realm.ExportRequest) (string, *zip.Reader, error) {
			return "", nil, errors.New("something bad happened")
		}

		cmd := &Command{inputs{From: "somewhere"}}

		err := cmd.Handler(nil, ui, cli.Clients{Realm: realmClient})
		assert.Equal(t, errors.New("something bad happened"), err)
	})

	t.Run("with a successful export", func(t *testing.T) {
		zipPkg, zipErr := zip.OpenReader("testdata/test.zip")
		assert.Nil(t, zipErr)
		defer zipPkg.Close()

		var realmClient mock.RealmClient
		realmClient.FindAppsFn = func(filter realm.AppFilter) ([]realm.App, error) {
			return nil, nil
		}
		realmClient.ExportFn = func(groupID, appID string, req realm.ExportRequest) (string, *zip.Reader, error) {
			return "app_20210101", &zipPkg.Reader, nil
		}

		t.Run("should not write any contents to the destination in a dry run", func(t *testing.T) {
			profile := mock.NewProfile(t)

			out, ui := mock.NewUI()

			cmd := &Command{inputs{DryRun: true, To: "app"}}

			assert.Nil(t, cmd.Handler(profile, ui, cli.Clients{Realm: realmClient}))
			destination := filepath.Join(profile.WorkingDirectory, "app")

			assert.Equal(t, `01:23:45 UTC INFO  No changes were written to your file system
01:23:45 UTC DEBUG Contents would have been written to: app
`, out.String())

			_, err := os.Stat(destination)
			assert.True(t, os.IsNotExist(err), "expected %s to not exist, but instead: %s", err)
		})

		t.Run("should write the received zip package to the destination", func(t *testing.T) {
			profile, teardown := mock.NewProfileFromTmpDir(t, "pull_handler_test")
			defer teardown()

			out, ui := mock.NewUI()

			cmd := &Command{inputs{To: "app"}}

			assert.Nil(t, cmd.Handler(profile, ui, cli.Clients{Realm: realmClient}))
			destination := filepath.Join(profile.WorkingDirectory, "app")

			assert.Equal(t, `01:23:45 UTC INFO  Saved app to disk
01:23:45 UTC INFO  Successfully pulled app down: app
`, out.String())

			_, err := os.Stat(destination)
			assert.Nil(t, err)

			testData, readErr := ioutil.ReadFile(filepath.Join(destination, "test.json"))
			assert.Nil(t, readErr)
			assert.Equal(t, "{\"egg\":\"corn\"}\n", string(testData))
		})
	})

	t.Run("with a realm client that fails to export dependencies", func(t *testing.T) {
		zipPkg, zipErr := zip.OpenReader("testdata/test.zip")
		assert.Nil(t, zipErr)
		defer zipPkg.Close()

		var realmClient mock.RealmClient
		realmClient.FindAppsFn = func(filter realm.AppFilter) ([]realm.App, error) {
			return nil, nil
		}
		realmClient.ExportFn = func(groupID, appID string, req realm.ExportRequest) (string, *zip.Reader, error) {
			return "app_20210101", &zipPkg.Reader, nil
		}
		realmClient.ExportDependenciesFn = func(groupID, appID string) (string, io.ReadCloser, error) {
			return "", nil, errors.New("something bad happened")
		}

		t.Run("not attempt to export dependencies if the flag is not set", func(t *testing.T) {
			profile, teardown := mock.NewProfileFromTmpDir(t, "pull_handler_test")
			defer teardown()

			out, ui := mock.NewUI()

			cmd := &Command{inputs{To: "app"}}

			assert.Nil(t, cmd.Handler(profile, ui, cli.Clients{Realm: realmClient}))

			assert.Equal(t, `01:23:45 UTC INFO  Saved app to disk
01:23:45 UTC INFO  Successfully pulled app down: app
`, out.String())
		})

		t.Run("return the error when export dependencies fails", func(t *testing.T) {
			profile, teardown := mock.NewProfileFromTmpDir(t, "pull_handler_test")
			defer teardown()

			_, ui := mock.NewUI()

			cmd := &Command{inputs{To: "app", IncludeDependencies: true}}

			err := cmd.Handler(profile, ui, cli.Clients{Realm: realmClient})
			assert.Equal(t, errors.New("something bad happened"), err)
		})
	})

	t.Run("with a realm client that successfully exports dependencies should write the archive file", func(t *testing.T) {
		profile, teardown := mock.NewProfileFromTmpDir(t, "pull_handler_test")
		defer teardown()

		out, ui := mock.NewUI()

		zipPkg, zipErr := zip.OpenReader("testdata/test.zip")
		assert.Nil(t, zipErr)
		defer zipPkg.Close()

		depsPkg, err := os.Open("testdata/node_modules.zip")
		assert.Nil(t, err)
		defer depsPkg.Close()

		var realmClient mock.RealmClient
		realmClient.FindAppsFn = func(filter realm.AppFilter) ([]realm.App, error) {
			return nil, nil
		}
		realmClient.ExportFn = func(groupID, appID string, req realm.ExportRequest) (string, *zip.Reader, error) {
			return "app_20210101", &zipPkg.Reader, nil
		}
		realmClient.ExportDependenciesFn = func(groupID, appID string) (string, io.ReadCloser, error) {
			return "node_modules.zip", depsPkg, nil
		}

		cmd := &Command{inputs{To: "app", IncludeDependencies: true}}

		assert.Nil(t, cmd.Handler(profile, ui, cli.Clients{Realm: realmClient}))
		assert.Equal(t, `01:23:45 UTC INFO  Saved app to disk
01:23:45 UTC INFO  Fetched dependencies archive
01:23:45 UTC INFO  Successfully pulled app down: app
`, out.String())

		_, pkgErr := os.Stat(filepath.Join(profile.WorkingDirectory, "app", local.NameFunctions, "node_modules.zip"))
		assert.Nil(t, pkgErr)
	})
}

func TestPullCommandDoExport(t *testing.T) {
	t.Run("should return an error if the export fails", func(t *testing.T) {
		groupID, appID := "groupID", "appID"

		var realmClient mock.RealmClient

		var capturedGroupID, capturedAppID string
		var capturedExportReq realm.ExportRequest
		realmClient.ExportFn = func(groupID, appID string, req realm.ExportRequest) (string, *zip.Reader, error) {
			capturedGroupID = groupID
			capturedAppID = appID
			capturedExportReq = req
			return "", nil, errors.New("something bad happened")
		}

		cmd := &Command{inputs{AppVersion: realm.AppConfigVersion20210101}}

		_, _, err := cmd.doExport(nil, realmClient, groupID, appID)
		assert.Equal(t, errors.New("something bad happened"), err)

		t.Log("and should properly pass through the expected args")
		assert.Equal(t, groupID, capturedGroupID)
		assert.Equal(t, appID, capturedAppID)
		assert.Equal(t, realm.ExportRequest{ConfigVersion: realm.AppConfigVersion20210101}, capturedExportReq)
	})

	t.Run("should return the expected destination file path", func(t *testing.T) {
		profile := mock.NewProfile(t)
		profile.WorkingDirectory = "/some/system/path"

		for _, tc := range []struct {
			description  string
			flagTo       string
			zipName      string
			expectedPath string
		}{
			{
				description:  "with a to flag set",
				flagTo:       "../../my-project",
				expectedPath: "/some/my-project",
			},
			{
				description:  "with no to flag set and the zip file name has a timestamp",
				zipName:      "app_20210101",
				expectedPath: "/some/system/path/app",
			},
			{
				description:  "with no to flag set and the zip file name has no timestamp",
				zipName:      "app-abcde",
				expectedPath: "/some/system/path/app-abcde",
			},
		} {
			t.Run(tc.description, func(t *testing.T) {
				var realmClient mock.RealmClient
				realmClient.ExportFn = func(groupID, appID string, req realm.ExportRequest) (string, *zip.Reader, error) {
					return tc.zipName, &zip.Reader{}, nil
				}

				cmd := &Command{inputs{To: tc.flagTo}}

				path, zipPkg, err := cmd.doExport(profile, realmClient, "", "")
				assert.Nil(t, err)
				assert.NotNil(t, zipPkg)
				assert.Equal(t, tc.expectedPath, path)
			})
		}
	})
}

func TestPullCommandCheckAppDestination(t *testing.T) {
	t.Run("should return true early if auto confirm is on", func(*testing.T) {
		ui := mock.NewUIWithOptions(mock.UIOptions{AutoConfirm: true}, new(bytes.Buffer))

		ok, err := checkAppDestination(ui, "")
		assert.Nil(t, err)
		assert.True(t, ok, "should be ok")
	})

	t.Run("should return true early if the path does not exist", func(t *testing.T) {
		_, ui := mock.NewUI()

		ok, err := checkAppDestination(ui, "./not_a_directory")
		assert.Nil(t, err)
		assert.True(t, ok, "should be ok")
	})

	t.Run("should return true if the path does exist but is a file", func(t *testing.T) {
		tmpDir, teardown, err := u.NewTempDir("pull-command")
		assert.Nil(t, err)
		defer teardown()

		file, err := os.Create(filepath.Join(tmpDir, "project"))
		assert.Nil(t, err)
		defer file.Close()

		_, ui := mock.NewUI()

		ok, err := checkAppDestination(ui, filepath.Join(tmpDir, "project"))
		assert.Nil(t, err)
		assert.True(t, ok, "should be ok")
	})

	t.Run("should prompt the user to continue if the directory already exists", func(t *testing.T) {
		tmpDir, teardown, err := u.NewTempDir("pull-command")
		assert.Nil(t, err)
		defer teardown()

		dir := filepath.Join(tmpDir, "project")

		assert.Nil(t, os.MkdirAll(dir, os.ModePerm))

		for _, tc := range []struct {
			description string
			input       string
			answer      bool
		}{
			{"yes", "y", true},
			{description: "no"},
		} {
			t.Run(fmt.Sprintf("and return %t with an answer of '%s'", tc.answer, tc.description), func(t *testing.T) {
				_, console, _, ui, err := mock.NewVT10XConsole()
				assert.Nil(t, err)
				defer console.Close()

				doneCh := make(chan (struct{}))
				go func() {
					defer close(doneCh)
					console.ExpectString(fmt.Sprintf("Directory '%s' already exists, do you still wish to proceed?", dir))
					console.SendLine(tc.input)
					console.ExpectEOF()
				}()

				ok, err := checkAppDestination(ui, dir)
				assert.Nil(t, err)
				assert.Equal(t, tc.answer, ok)
			})
		}
	})
}