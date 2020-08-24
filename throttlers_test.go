package gohalt

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	ms0_9 time.Duration = time.Duration(0.9 * float64(time.Millisecond))
	ms1_0 time.Duration = time.Millisecond
	ms1_5 time.Duration = time.Duration(1.5 * float64(time.Millisecond))
	ms2_0 time.Duration = 2 * time.Millisecond
	ms3_0 time.Duration = 3 * time.Millisecond
	ms5_0 time.Duration = 5 * time.Millisecond
	ms7_0 time.Duration = 7 * time.Millisecond
)

type tcase struct {
	tms  uint64
	thr  Throttler
	acts []Runnable
	pres []Runnable
	ctxs []context.Context
	errs []error
	durs []time.Duration
	idx  int64
	over bool
}

func (t *tcase) run(index int) (err error, dur time.Duration) {
	// get context with fallback
	ctx := context.Background()
	if index < len(t.ctxs) {
		ctx = t.ctxs[index]
	}
	// run additional pre action only if present
	if index < len(t.pres) {
		if pre := t.pres[index]; pre != nil {
			_ = pre(ctx)
		}
	}
	var ts time.Time
	// try catch panic into error
	func() {
		defer func() {
			if msg := recover(); msg != nil {
				atomic.AddInt64(&t.idx, 1)
				err = errors.New(msg.(string))
			}
		}()
		ts = time.Now()
		// force strict acquire order
		for index != int(atomic.LoadInt64(&t.idx)) {
			time.Sleep(time.Microsecond)
		}
		err = t.thr.Acquire(ctx)
		atomic.AddInt64(&t.idx, 1)
	}()
	dur = time.Since(ts)
	// run additional action only if present
	if index < len(t.acts) {
		if act := t.acts[index]; act != nil {
			_ = act(ctx)
		}
	}
	limit := 1
	if t.over { // imitate over releasing
		limit = index + 1
	}
	for i := 0; i < limit; i++ {
		if err := t.thr.Release(ctx); err != nil {
			return err, dur
		}
	}
	return
}

func (t *tcase) result(index int) (err error, dur time.Duration) {
	if index < len(t.errs) {
		err = t.errs[index]
	}
	if index < len(t.durs) {
		dur = t.durs[index]
	}
	return
}

func TestThrottlerPattern(t *testing.T) {
	table := map[string]tcase{
		"Throttler echo should not throttle on nil input": {
			tms: 3,
			thr: NewThrottlerEcho(nil),
		},
		"Throttler echo should throttle on not nil input": {
			tms: 3,
			thr: NewThrottlerEcho(errors.New("test")),
			errs: []error{
				errors.New("test"),
				errors.New("test"),
				errors.New("test"),
			},
		},
		"Throttler wait should sleep for millisecond": {
			tms: 3,
			thr: NewThrottlerWait(ms1_0),
			durs: []time.Duration{
				ms0_9,
				ms0_9,
				ms0_9,
			},
		},
		"Throttler backoff should sleep for correct time periods": {
			tms: 5,
			thr: NewThrottlerBackoff(ms1_0, 20*ms1_0, true),
			durs: []time.Duration{
				ms0_9,
				ms0_9 * 4,
				ms0_9 * 9,
				ms0_9 * 16,
				ms0_9,
			},
		},
		"Throttler panic should panic": {
			tms: 3,
			thr: NewThrottlerPanic(),
			errs: []error{
				errors.New("throttler has reached panic"),
				errors.New("throttler has reached panic"),
				errors.New("throttler has reached panic"),
			},
		},
		"Throttler each should throttle on threshold": {
			tms: 6,
			thr: NewThrottlerEach(3),
			errs: []error{
				nil,
				nil,
				errors.New("throttler has reached periodic threshold"),
				nil,
				nil,
				errors.New("throttler has reached periodic threshold"),
			},
		},
		"Throttler before should throttle before threshold": {
			tms: 6,
			thr: NewThrottlerBefore(3),
			errs: []error{
				errors.New("throttler has not reached threshold yet"),
				errors.New("throttler has not reached threshold yet"),
				errors.New("throttler has not reached threshold yet"),
				nil,
				nil,
				nil,
			},
		},
		"Throttler chance should throttle on 1": {
			tms: 3,
			thr: NewThrottlerChance(1),
			errs: []error{
				errors.New("throttler has reached chance threshold"),
				errors.New("throttler has reached chance threshold"),
				errors.New("throttler has reached chance threshold"),
			},
		},
		"Throttler chance should throttle on >1": {
			tms: 3,
			thr: NewThrottlerChance(10.10),
			errs: []error{
				errors.New("throttler has reached chance threshold"),
				errors.New("throttler has reached chance threshold"),
				errors.New("throttler has reached chance threshold"),
			},
		},
		"Throttler chance should not throttle on 0": {
			tms: 3,
			thr: NewThrottlerChance(0),
		},
		"Throttler after should throttle after threshold": {
			tms: 6,
			thr: NewThrottlerAfter(3),
			errs: []error{
				nil,
				nil,
				nil,
				errors.New("throttler has exceed threshold"),
				errors.New("throttler has exceed threshold"),
				errors.New("throttler has exceed threshold"),
			},
		},
		"Throttler running should throttle on threshold": {
			tms: 3,
			thr: NewThrottlerRunning(1),
			acts: []Runnable{
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
			},
			errs: []error{
				nil,
				errors.New("throttler has exceed running threshold"),
				errors.New("throttler has exceed running threshold"),
			},
			over: true,
		},
		"Throttler buffered should throttle on threshold": {
			tms: 3,
			thr: NewThrottlerBuffered(1),
			acts: []Runnable{
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
			},
			durs: []time.Duration{
				0,
				ms0_9,
				ms0_9,
			},
			over: true,
		},
		"Throttler priority should throttle on threshold": {
			tms: 3,
			thr: NewThrottlerPriority(1, 0),
			acts: []Runnable{
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
			},
			durs: []time.Duration{
				0,
				ms0_9,
				ms0_9,
			},
			over: true,
		},
		"Throttler priority should not throttle on priority": {
			tms: 7,
			thr: NewThrottlerPriority(5, 2),
			acts: []Runnable{
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
			},
			ctxs: []context.Context{
				WithPriority(context.Background(), 1),
				WithPriority(context.Background(), 1),
				WithPriority(context.Background(), 1),
				WithPriority(context.Background(), 2),
				WithPriority(context.Background(), 2),
				WithPriority(context.Background(), 2),
				WithPriority(context.Background(), 2),
			},
			durs: []time.Duration{
				0,
				0,
				ms0_9,
				0,
				0,
				0,
				ms0_9,
			},
		},
		"Throttler timed should throttle after threshold": {
			tms: 6,
			thr: NewThrottlerTimed(
				2,
				ms1_0,
				0,
			),
			acts: []Runnable{
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
			},
			pres: []Runnable{
				nil,
				nil,
				nil,
				nil,
				delayed(ms2_0, nope),
				delayed(ms2_0, nope),
			},
			errs: []error{
				nil,
				nil,
				errors.New("throttler has exceed threshold"),
				errors.New("throttler has exceed threshold"),
				nil,
				nil,
			},
		},
		"Throttler timed should throttle after threshold with quantum": {
			tms: 6,
			thr: NewThrottlerTimed(
				2,
				ms2_0,
				ms1_0,
			),
			acts: []Runnable{
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
				delayed(ms1_0, nope),
			},
			pres: []Runnable{
				nil,
				nil,
				nil,
				delayed(ms1_5, nope),
				nil,
				delayed(ms2_0, nope),
			},
			errs: []error{
				nil,
				nil,
				errors.New("throttler has exceed threshold"),
				nil,
				errors.New("throttler has exceed threshold"),
				nil,
			},
		},
		"Throttler monitor should throttle with error on internal error": {
			tms: 3,
			thr: NewThrottlerMonitor(
				mntmock{err: errors.New("test")},
				Stats{},
			),
			errs: []error{
				fmt.Errorf("throttler hasn't found any stats %w", errors.New("test")),
				fmt.Errorf("throttler hasn't found any stats %w", errors.New("test")),
				fmt.Errorf("throttler hasn't found any stats %w", errors.New("test")),
			},
		},
		"Throttler monitor should not throttle on stats below threshold": {
			tms: 3,
			thr: NewThrottlerMonitor(
				mntmock{
					stats: Stats{
						MEMAlloc:  100,
						MEMSystem: 1000,
						CPUPause:  100,
						CPUUsage:  0.1,
					},
				},
				Stats{
					MEMAlloc:  1000,
					MEMSystem: 2000,
					CPUPause:  500,
					CPUUsage:  0.3,
				},
			),
		},
		"Throttler monitor should throttle on stats above threshold": {
			tms: 3,
			thr: NewThrottlerMonitor(
				mntmock{
					stats: Stats{
						MEMAlloc:  500,
						MEMSystem: 5000,
						CPUPause:  500,
						CPUUsage:  0.1,
					},
				},
				Stats{
					MEMAlloc:  1000,
					MEMSystem: 2000,
					CPUPause:  500,
					CPUUsage:  0.3,
				},
			),
			errs: []error{
				errors.New("throttler has exceed stats threshold"),
				errors.New("throttler has exceed stats threshold"),
				errors.New("throttler has exceed stats threshold"),
			},
		},
		"Throttler metric should throttle with error on internal error": {
			tms: 3,
			thr: NewThrottlerMetric(mtcmock{err: errors.New("test")}),
			errs: []error{
				fmt.Errorf("throttler hasn't found any metric %w", errors.New("test")),
				fmt.Errorf("throttler hasn't found any metric %w", errors.New("test")),
				fmt.Errorf("throttler hasn't found any metric %w", errors.New("test")),
			},
		},
		"Throttler monitor should not throttle on metric below threshold": {
			tms: 3,
			thr: NewThrottlerMetric(mtcmock{metric: false}),
		},
		"Throttler monitor should throttle on metric above threshold": {
			tms: 3,
			thr: NewThrottlerMetric(mtcmock{metric: true}),
			errs: []error{
				errors.New("throttler has reached metric threshold"),
				errors.New("throttler has reached metric threshold"),
				errors.New("throttler has reached metric threshold"),
			},
		},
		"Throttler latency should throttle on latency above threshold": {
			tms: 3,
			thr: NewThrottlerLatency(ms0_9, ms5_0),
			ctxs: []context.Context{
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms5_0).UTC().UnixNano()),
				context.Background(),
				context.Background(),
			},
			errs: []error{
				nil,
				errors.New("throttler has exceed latency threshold"),
				errors.New("throttler has exceed latency threshold"),
			},
		},
		"Throttler latency should not throttle on latency above threshold after retention": {
			tms: 3,
			thr: NewThrottlerLatency(ms0_9, ms3_0),
			ctxs: []context.Context{
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms5_0).UTC().UnixNano()),
				context.Background(),
				context.Background(),
			},
			pres: []Runnable{
				nil,
				nil,
				delayed(ms5_0, nope),
			},
			errs: []error{
				nil,
				errors.New("throttler has exceed latency threshold"),
				nil,
			},
		},
		"Throttler percentile should throttle on latency above threshold": {
			tms: 5,
			thr: NewThrottlerPercentile(ms3_0, 0.5, ms5_0),
			ctxs: []context.Context{
				context.Background(),
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms5_0).UTC().UnixNano()),
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms5_0).UTC().UnixNano()),
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms1_0).UTC().UnixNano()),
				context.Background(),
			},
			errs: []error{
				nil,
				nil,
				errors.New("throttler has exceed latency threshold"),
				errors.New("throttler has exceed latency threshold"),
				errors.New("throttler has exceed latency threshold"),
			},
		},
		"Throttler percentile should throttle on latency above threshold after retention": {
			tms: 5,
			thr: NewThrottlerPercentile(ms3_0, 1.5, ms5_0),
			ctxs: []context.Context{
				context.Background(),
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms5_0).UTC().UnixNano()),
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms5_0).UTC().UnixNano()),
				context.WithValue(context.Background(), ghctxtimestamp, time.Now().Add(-ms1_0).UTC().UnixNano()),
				context.Background(),
			},
			pres: []Runnable{
				nil,
				nil,
				nil,
				nil,
				delayed(ms7_0, nope),
			},
			errs: []error{
				nil,
				nil,
				errors.New("throttler has exceed latency threshold"),
				errors.New("throttler has exceed latency threshold"),
				nil,
			},
		},
	}
	for tname, tcase := range table {
		t.Run(tname, func(t *testing.T) {
			var index int64
			for i := 0; i < int(tcase.tms); i++ {
				t.Run(fmt.Sprintf("run %d", i+1), func(t *testing.T) {
					t.Parallel()
					index := int(atomic.AddInt64(&index, 1) - 1)
					resErr, resDur := tcase.result(index)
					err, dur := tcase.run(index)
					assert.Equal(t, resErr, err)
					assert.LessOrEqual(t, int64(resDur/2), int64(dur))
				})
			}
		})
	}
}
