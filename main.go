package main

import (
	"context"
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

type StatusResponse struct {
	PendingJobs uint64 `json:"pending_jobs"`
	ActiveJobs  uint64 `json:"active_jobs"`
}

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
				for key, val := range secrets.Load().(map[string]string) {
					signEnv = append(signEnv, key+"="+val)
				}
				// Ensure Python prints are immediately flushed
				signEnv = append(signEnv, "PYTHONUNBUFFERED=1")
				cmd := exec.CommandContext(ctx, filepath.Join(workDir, *entrypoint))
				cmd.Dir = workDir
				cmd.Env = signEnv
				if output, err := cmd.CombinedOutput(); err != nil {
					return errors.WithMessage(errors.WithMessage(errors.New(string(output)), err.Error()), "sign script")
				}
				return nil
			}()
			if err != nil {
				log.Println(id, err)
			}
			log.Println(id, "Finished sign job")
			cancel()
			<-workerChan
		}
	}()

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())

	keyAuth := middleware.KeyAuth(func(s string, c echo.Context) (bool, error) {
		return s == *authKey, nil
	})

	e.GET("/status", func(c echo.Context) error {
		return c.JSONPretty(200, StatusResponse{
			PendingJobs: uint64(len(jobChan)),
			ActiveJobs:  uint64(len(workerChan)),
		}, "  ")
	})
	e.POST("/trigger", func(c echo.Context) error {
		select {
		case jobChan <- true:
			return c.NoContent(200)
		default:
			return errors.New("job queue full")
		}
	}, keyAuth)
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
	}, keyAuth)

	return e.Start(fmt.Sprintf("%s:%d", *host, *port))
}
