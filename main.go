package main

import (
	"context"
	"crypto/subtle"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	dirCopy "github.com/otiai10/copy"
	"github.com/pkg/errors"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

var secrets atomic.Value

// lastRun is the most recent sign job's *runLog, shown by /status.
var lastRun atomic.Pointer[runLog]

func main() {
	if err := mainE(); err != nil {
		log.Fatalln(err)
	}
}

func mainE() error {
	host := flag.String("host", "", "Listen host, empty for all")
	port := flag.Uint64("port", 8090, "Listen port")
	signFilesDir := flag.String("files", "", "Path to directory whose files "+
		"will be included in each sign job. Should at least contain a signer script 'sign.sh'")
	authKey := flag.String("key", "", "Auth key the web service must use to talk to this server")
	jobTimeout := flag.Uint64("timeout", 15, "Job timeout in minutes")
	entrypoint := flag.String("entrypoint", "sign.py", "Entrypoint script to run when signing")
	flag.Parse()

	if *signFilesDir == "" || *authKey == "" {
		flag.Usage()
		return errors.New("missing argument(s)")
	}

	if stat, err := os.Stat(*signFilesDir); err != nil {
		return errors.WithMessage(err, "stat sign files dir")
	} else if !stat.IsDir() {
		return errors.New("sign files dir not a directory")
	}
	if len(strings.TrimSpace(*authKey)) < 8 {
		return errors.New("auth key must be at least 8 characters long")
	}

	jobChan := make(chan bool, 1000)
	workerChan := make(chan bool, 1)
	go func() {
		for {
			<-jobChan
			workerChan <- true
			id := uuid.NewString()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*jobTimeout)*time.Minute)
			log.Println(id, "Running sign job")
			run := newRunLog()
			lastRun.Store(run)
			err := func() error {
				tempDir, err := os.MkdirTemp(".", "ios-signer")
				if err != nil {
					return errors.WithMessage(err, "make temp dir")
				}
				defer os.RemoveAll(tempDir)
				workDir, err := filepath.Abs(tempDir)
				if err != nil {
					return errors.WithMessage(err, "get sign job dir absolute path")
				}
				if err := dirCopy.Copy(*signFilesDir, workDir); err != nil {
					return errors.WithMessage(err, "copy sign files")
				}
				signEnv := os.Environ()
				// none if the web service hasn't sent them yet, so the script fails cleanly instead
				currentSecrets, _ := secrets.Load().(map[string]string)
				for key, val := range currentSecrets {
					signEnv = append(signEnv, key+"="+val)
				}
				// Ensure Python prints are immediately flushed
				signEnv = append(signEnv, "PYTHONUNBUFFERED=1")
				cmd := exec.CommandContext(ctx, filepath.Join(workDir, *entrypoint))
				cmd.Dir = workDir
				cmd.Env = signEnv
				cmd.Stdout = run
				cmd.Stderr = run
				if err := cmd.Run(); err != nil {
					if ctx.Err() != nil {
						err = errors.WithMessage(err, "timed out")
					}
					return errors.WithMessage(err, "sign script")
				}
				return nil
			}()
			run.finish(err)
			if err != nil {
				log.Println(id, err)
				log.Println(id, "Sign job output:\n"+run.String())
			}
			log.Println(id, "Finished sign job")
			cancel()
			<-workerChan
		}
	}()

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())

	// every route needs the key, which only the web service has, to keep the attack surface minimal
	e.Use(middleware.KeyAuth(func(s string, c echo.Context) (bool, error) {
		return subtle.ConstantTimeCompare([]byte(s), []byte(*authKey)) == 1, nil
	}))

	e.GET("/status", func(c echo.Context) error {
		res := c.Response()
		res.Header().Set(echo.HeaderContentType, echo.MIMETextPlainCharsetUTF8)
		res.Header().Set("Cache-Control", "no-store")
		res.WriteHeader(200)
		fmt.Fprintf(res, "Pending jobs: %d\nActive jobs: %d\n\n", len(jobChan), len(workerChan))
		run := lastRun.Load()
		if run == nil {
			fmt.Fprintln(res, "No jobs have run yet.")
			return nil
		}
		return run.follow(c.Request().Context(), res)
	})
	e.POST("/trigger", func(c echo.Context) error {
		select {
		case jobChan <- true:
			return c.NoContent(200)
		default:
			return errors.New("job queue full")
		}
	})
	e.POST("/secrets", func(c echo.Context) error {
		bodyBytes, err := ioutil.ReadAll(c.Request().Body)
		if err != nil {
			c.Response().WriteHeader(400)
			return err
		}
		params, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			c.Response().WriteHeader(400)
			return err
		}
		var newSecrets = map[string]string{}
		for key, val := range params {
			newSecrets[key] = val[0]
		}
		secrets.Store(newSecrets)
		return c.NoContent(200)
	})

	return e.Start(fmt.Sprintf("%s:%d", *host, *port))
}
