// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package manager

import (
	"context"
	"sync"
	"time"
)

const (
	SonataFlowControllerWorkerSize = 100
)

var (
	sonataFlowControllerWorker *Worker
	operatorStarTime           time.Time
)

type Runnable func()

type Worker struct {
	ch chan Runnable
}

func SetOperatorStartTime() {
	operatorStarTime = time.Now()
}

func GetOperatorStartTime() time.Time {
	return operatorStarTime
}

func GetSFCWorker() *Worker {
	return sonataFlowControllerWorker
}

func InitializeSFCWorker(size int) *Worker {
	worker := NewWorker(size)
	worker.Start()
	sonataFlowControllerWorker = &worker
	return sonataFlowControllerWorker
}

func NewWorker(size int) Worker {
	return Worker{ch: make(chan Runnable, size)}
}

func (w Worker) Start() {
	go func(ch chan Runnable) {
		for {
			r, ok := <-ch
			if !ok {
				break
			} else {
				r()
			}
		}
	}(w.ch)
}

func (w Worker) RunAsync(r Runnable) {
	w.ch <- r
}

var sonataFlowPlatformControllerWorkerRegistry *PeriodicWorkerRegistry

func InitializeSFPControllerWorkerRegistry(rootCtx context.Context) {
	sonataFlowPlatformControllerWorkerRegistry = &PeriodicWorkerRegistry{
		rootCtx: rootCtx,
		workers: make(map[string]*PeriodicWorker),
	}
}

func GetSFPControllerWorkerRegistry() *PeriodicWorkerRegistry {
	return sonataFlowPlatformControllerWorkerRegistry
}

type PeriodicWorkerRegistry struct {
	rootCtx context.Context
	workers map[string]*PeriodicWorker
	mu      sync.RWMutex
}

func (m *PeriodicWorkerRegistry) GetRootContext() context.Context {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rootCtx
}

func (m *PeriodicWorkerRegistry) Exists(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.workers[name]
	return exists
}

func (m *PeriodicWorkerRegistry) GetIfExists(name string) *PeriodicWorker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	worker, exists := m.workers[name]
	if exists {
		return worker
	}
	return nil
}

func (m *PeriodicWorkerRegistry) Register(name string, worker *PeriodicWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers[name] = worker
}

func (m *PeriodicWorkerRegistry) Deregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.workers, name)
}

type ContextAwareRunnable func(ctx context.Context)

type PeriodicWorker struct {
	mu              sync.Mutex
	periodInSeconds int
	running         bool
	work            ContextAwareRunnable
	execTimeout     time.Duration
	cancel          context.CancelFunc
	ctx             context.Context
}

func NewPeriodicWorker(r ContextAwareRunnable, periodInSeconds int, execTimeout time.Duration) *PeriodicWorker {
	return &PeriodicWorker{
		work:            r,
		periodInSeconds: periodInSeconds,
		execTimeout:     execTimeout,
	}
}

func (w *PeriodicWorker) Start(parentCtx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return
	}
	w.ctx, w.cancel = context.WithCancel(parentCtx)
	w.running = true
	go w.run()
}

// run used by Start, never call it directly.
func (w *PeriodicWorker) run() {
	ticker := time.NewTicker(time.Duration(w.periodInSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.executeOnce()
		}
	}
}

// executeOnce used by run, never call it directly.
func (w *PeriodicWorker) executeOnce() {
	// execution warded by the respective worker execTimeout
	execCtx, cancel := context.WithTimeout(w.ctx, w.execTimeout)
	defer cancel()
	w.work(execCtx)
}

func (w *PeriodicWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return
	}
	if w.cancel != nil {
		w.cancel()
	}
	w.cancel = nil
	w.running = false
}
