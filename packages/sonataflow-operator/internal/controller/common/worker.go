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

package common

import (
	"fmt"
	"time"
)

var (
	sonataFlowControllerWorker *Worker

	operatorStarTime time.Time
)

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

type Runnable func() error

type Worker struct {
	ch chan Runnable
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
				if err := r(); err != nil {
					fmt.Printf("An error was produced during Runnable excution: %s\n", err.Error())
				}
			}
		}
	}(w.ch)
}

func (w Worker) RunAsync(r Runnable) {
	fmt.Printf("%s - Worker, programming runnable\n", time.Now().UTC().String())
	w.ch <- r
}

func (w Worker) Len() int {
	return len(w.ch)
}
