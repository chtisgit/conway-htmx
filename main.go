package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
)

func sendFullGameField(w io.Writer, game *Game, templates *template.Template, rows, cols int) (int64, error) {
	game.lock.Lock()
	defer game.lock.Unlock()

	step := game.steps

	fmt.Fprintf(w, "event: all\r\ndata:")
	err := templates.ExecuteTemplate(stripNewlines(w), "field", FieldData{
		Rows:  rows,
		Cols:  cols,
		Cells: game.field,
	})
	fmt.Fprintf(w, "\r\n\r\n")

	if err != nil {
		log.Printf("/game/field error: %s (clientID=%d)\n", err.Error())
	}

	return step, err
}

func initQueue(tmpDir string) *Queue {
	q, err := newQueue(tmpDir)
	if err != nil {
		log.Fatalf("error creating queue: %s\n", err.Error())
	}

	return q
}

func main() {
	var listenAddr string
	var tmpDir string
	flag.StringVar(&listenAddr, "listen", ":8000", "ip and port to listen on seperated by colon, ip might be empty")
	flag.StringVar(&tmpDir, "tmpdir", "", "directory for temporary data (default is system tmp directory)")
	flag.Parse()

	cols, rows := 40, 25
	mux := http.NewServeMux()
	game := NewGame(cols, rows)
	q := initQueue(tmpDir)

	templates, err := template.New("").Funcs(template.FuncMap{
		"makeCell": func(x, y int, val byte) Cell {
			return Cell{
				X:   x,
				Y:   y,
				Val: val,
			}
		},
	}).Parse(templatesSource)
	if err != nil {
		log.Fatalln("error: cannot parse field template: ", err.Error())
	}

	type event struct {
		step  int64
		event []byte
	}
	newEvents := make(chan event, 2)
	defer close(newEvents)

	game.OnStep(func(game *Game, step int64) {
		var eventBuilder bytes.Buffer

		changes := 0
		for y := 0; y != rows; y++ {
			for x := 0; x != cols; x++ {
				if game.field[y][x] != game.fieldCopy[y][x] {
					cell := Cell{
						X:   x,
						Y:   y,
						Val: game.field[y][x],
					}
					eventBuilder.WriteString("event: ")
					templates.ExecuteTemplate(&eventBuilder, "cell-id", cell)
					eventBuilder.WriteString("\r\ndata:")
					templates.ExecuteTemplate(stripNewlines(&eventBuilder), "cell", cell)
					eventBuilder.WriteString("\r\n\r\n")
					changes++
				}
			}
		}

		ev := eventBuilder.Bytes()
		log.Printf("pushing step with %d changes and event.length of %d\n", changes, len(ev))
		newEvents <- event{step: game.steps, event: ev}
	})
	go func() {
		for ev := range newEvents {
			if err := q.PushStep(ev.step, ev.event); err != nil {
				log.Printf("error when pushing step %d: %s\n", game.steps, err.Error())
			}
		}
	}()

	clientID := int32(0)
	mux.HandleFunc("/game/sse", func(w http.ResponseWriter, r *http.Request) {
		acceptEnc := r.Header.Get("Accept-Encoding")
		if !strings.Contains(acceptEnc, "deflate") {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}

		myClientID := atomic.AddInt32(&clientID, 1)
		log.Printf("/game/sse request received (clientID=%d)\n", myClientID)
		defer log.Printf("/game/sse request ended (clientID=%d)\n", myClientID)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Encoding", "deflate")
		w.Header().Set("Transfer-Encoding", "chunked")

		flusher := w.(http.Flusher)
		zw := zlib.NewWriter(w)
		w = nil
		defer zw.Close()

		log.Printf("/game/sse preparing initial message ... (clientID=%d)\n", myClientID)
		currentStep, err := sendFullGameField(zw, &game, templates, rows, cols)
		zw.Flush()
		flusher.Flush()
		log.Printf("/game/sse initial message sent. (clientID=%d)\n", myClientID)

		if err != nil {
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		steps := q.Steps(ctx, currentStep)

		for {
			select {
			case event := <-steps:
				log.Printf("/game/sse sending update... (clientID=%d)\n", myClientID)
				io.Copy(zw, bytes.NewReader(event))
				zw.Flush()
				flusher.Flush()
				log.Printf("/game/sse update sent. (clientID=%d)\n", myClientID)

			case <-r.Context().Done():
				return
			}
		}
	})

	mux.HandleFunc("/game/playpause", func(w http.ResponseWriter, r *http.Request) {
		wrResp := func(running bool) {
			if running {
				w.Write([]byte("Pause"))
			} else {
				w.Write([]byte("Play"))
			}
		}

		if r.Method == http.MethodGet {
			running := game.isRunning()
			wrResp(running)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not Allowed", http.StatusMethodNotAllowed)
			return
		}

		running := game.toggleRun()
		wrResp(running)
	})

	mux.HandleFunc("/game/step", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not Allowed", http.StatusMethodNotAllowed)
			return
		}

		game.nextStep()
	})

	mux.HandleFunc("/game/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not Allowed", http.StatusMethodNotAllowed)
			return
		}

		game.clear()
	})

	mux.HandleFunc("/game/cell", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not Allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			log.Printf("/game/cell: error parsing form: %s\n", err.Error())
			return
		}

		xs := r.Form.Get("x")
		ys := r.Form.Get("y")
		x, err := strconv.Atoi(xs)
		if err != nil {
			log.Printf("/game/cell: error x is not integer: %s\n", xs)
			return
		}
		y, err := strconv.Atoi(ys)
		if err != nil {
			log.Printf("/game/cell: error y is not integer: %s\n", ys)
			return
		}

		val := game.toggleCell(x, y)

		templates.ExecuteTemplate(w, "cell", Cell{
			X:   x,
			Y:   y,
			Val: val,
		})
	})

	mux.Handle("/", http.FileServer(http.Dir("web")))

	serverStoppedCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGABRT, syscall.SIGSEGV, syscall.SIGQUIT, syscall.SIGINT, syscall.SIGTERM)

	server := http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	go func() {
		defer close(serverStoppedCh)
		log.Printf("Ready. Listening on %s.\n", server.Addr)
		err = server.ListenAndServe()
		if err != nil {
			log.Fatalln("error: ", err)
		}
	}()

	select {
	case <-serverStoppedCh:
	case sig := <-sigCh:
		q.DeleteAllSteps()
		log.Printf("Signal %s caught. Data deleted. Stopping server...", sig.String())
		server.Close()
	}
}
