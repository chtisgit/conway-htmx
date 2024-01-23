package main

import (
	"log"
	"time"

	"github.com/sasha-s/go-deadlock"
)

type Cell struct {
	X, Y int
	Val  byte
}

type OnStepCallback = func(*Game, int64)
type callbackContainer struct {
	id int64
	cb OnStepCallback
}

type Game struct {
	lock deadlock.Mutex

	field     [][]byte
	fieldCopy [][]byte
	steps     int64
	stopCh    chan struct{}

	cbs []callbackContainer
}

func NewGame(cols, rows int) Game {
	field := make([][]byte, rows)
	for n := 0; n < rows; n++ {
		field[n] = make([]byte, cols)
	}
	fieldCopy := make([][]byte, rows)
	for n := 0; n < rows; n++ {
		fieldCopy[n] = make([]byte, cols)
	}

	return Game{
		field:     field,
		fieldCopy: fieldCopy,
	}
}

func (g *Game) neighborsAlive(field [][]byte, x, y int) int {
	n := 0
	if x >= 1 {
		if field[y][x-1] != 0 {
			n++
		}
		if y >= 1 && field[y-1][x-1] != 0 {
			n++
		}
		if y <= len(field)-2 && field[y+1][x-1] != 0 {
			n++
		}
	}
	if x <= len(field[y])-2 {
		if field[y][x+1] != 0 {
			n++
		}
		if y >= 1 && field[y-1][x+1] != 0 {
			n++
		}
		if y <= len(field)-2 && field[y+1][x+1] != 0 {
			n++
		}
	}
	if y >= 1 && field[y-1][x] != 0 {
		n++
	}
	if y <= len(field)-2 && field[y+1][x] != 0 {
		n++
	}

	return n
}

func (g *Game) notifyCallbacks() {
	for _, cc := range g.cbs {
		cc.cb(g, g.steps)
	}
}

func (g *Game) backupField() {
	for row := range g.field {
		copy(g.fieldCopy[row], g.field[row])
	}
}

func (g *Game) clear() {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.steps++
	g.backupField()

	for _, row := range g.field {
		for x := range row {
			row[x] = 0
		}
	}

	g.notifyCallbacks()
}

func (g *Game) nextStep() {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.steps++
	log.Printf("%d. game step", g.steps)
	defer log.Printf("%d. game step done", g.steps)

	g.backupField()

	for y, row := range g.fieldCopy {
		for x, val := range row {
			neighborsAlive := g.neighborsAlive(g.fieldCopy, x, y)
			if val == 0 && neighborsAlive == 3 {
				g.field[y][x] = 1
			} else if val != 0 && neighborsAlive != 2 && neighborsAlive != 3 {
				g.field[y][x] = 0
			}
		}
	}

	g.notifyCallbacks()
}

func (g *Game) OnStep(cb OnStepCallback) func() {
	g.lock.Lock()
	defer g.lock.Unlock()

	n := int64(0)
	for i := range g.cbs {
		if g.cbs[i].id > n {
			n = g.cbs[i].id
		}
	}
	n++

	g.cbs = append(g.cbs, callbackContainer{
		cb: cb,
		id: n,
	})

	return func() {
		g.lock.Lock()
		defer g.lock.Unlock()

		for i := range g.cbs {
			if g.cbs[i].id == n {
				g.cbs[i].cb = nil
				copy(g.cbs[i:], g.cbs[i+1:])
				g.cbs = g.cbs[:len(g.cbs)-1]
				break
			}
		}
	}

}

func (g *Game) toggleCell(x, y int) byte {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.backupField()
	g.steps++

	newVal := byte(1)
	if g.field[y][x] == 1 {
		newVal = 0
	}

	g.field[y][x] = newVal

	g.notifyCallbacks()

	return newVal
}

func (g *Game) start() {
	stopCh := make(chan struct{})
	g.stopCh = stopCh
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				g.nextStep()
			case <-stopCh:
				return
			}
		}
	}()
}

func (g *Game) stop() {
	close(g.stopCh)
	g.stopCh = nil
}

func (g *Game) isRunning() bool {
	g.lock.Lock()
	running := g.stopCh != nil
	g.lock.Unlock()

	return running
}

func (g *Game) toggleRun() bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	running := false
	if g.stopCh != nil {
		g.stop()
	} else {
		g.start()
		running = true
	}

	return running
}
