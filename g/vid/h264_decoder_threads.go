package vid

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type nativeCompensation struct {
	frame   Yuv420Frame
	part    nativePartition
	motion  MotionVector
	average bool
}

type nativeInterResidual struct {
	luma   [256]int64
	chroma [2][64]int64
}

// nativeReconstruction holds the sample work for one inter macroblock, deferred until parsing finishes it.
type nativeReconstruction struct {
	result        *CABACIIntraMacroblockResult
	builder       *Yuv420FrameBuilder
	address       int
	width         int
	compensations []nativeCompensation
	residual      *nativeInterResidual
	claimed       atomic.Bool
	done          chan struct{}
}

func (job *nativeReconstruction) run() error {
	for _, compensation := range job.compensations {
		if err := compensateNativePartition(job.result, compensation.frame, job.address, job.width, compensation.part, compensation.motion, compensation.average); err != nil {
			return err
		}
	}
	if job.residual != nil {
		for index, value := range job.residual.luma {
			addNativeResidual(&job.result.Luma[index], value)
		}
		for index := range job.residual.chroma[0] {
			addNativeResidual(&job.result.Cb[index], job.residual.chroma[0][index])
			addNativeResidual(&job.result.Cr[index], job.residual.chroma[1][index])
		}
	}
	return job.builder.PlaceMacroblock(job.address, job.result.Luma, job.result.Cb, job.result.Cr)
}

// nativeReconstructionPool runs inter macroblock reconstruction on worker goroutines.
// Jobs are independent of each other, so whichever goroutine claims a job first runs it.
// A nil pool runs every job inline on the caller.
type nativeReconstructionPool struct {
	jobs    chan *nativeReconstruction
	pending []*nativeReconstruction
	workers sync.WaitGroup
	mutex   sync.Mutex
	err     error
}

func newNativeReconstructionPool(threads, macroblocks int) *nativeReconstructionPool {
	if threads <= 1 {
		return nil
	}
	pool := &nativeReconstructionPool{jobs: make(chan *nativeReconstruction, threads*16), pending: make([]*nativeReconstruction, macroblocks)}
	jobs := pool.jobs
	pool.workers.Add(threads)
	for worker := 0; worker < threads; worker++ {
		go func() {
			defer pool.workers.Done()
			for job := range jobs {
				if job.claimed.CompareAndSwap(false, true) {
					pool.execute(job)
				}
			}
		}()
	}
	return pool
}

func (pool *nativeReconstructionPool) execute(job *nativeReconstruction) {
	if err := job.run(); err != nil {
		pool.mutex.Lock()
		if pool.err == nil {
			pool.err = err
		}
		pool.mutex.Unlock()
	}
	close(job.done)
}

func (pool *nativeReconstructionPool) submit(job *nativeReconstruction) error {
	if pool == nil {
		return job.run()
	}
	job.done = make(chan struct{})
	pool.pending[job.address] = job
	select {
	case pool.jobs <- job:
	default:
		// Workers are saturated; reconstruct here rather than block.
		job.claimed.Store(true)
		pool.execute(job)
	}
	return nil
}

// waitFor ensures the macroblock at address, if submitted, has been reconstructed.
func (pool *nativeReconstructionPool) waitFor(address int) {
	if pool == nil || address < 0 || address >= len(pool.pending) || pool.pending[address] == nil {
		return
	}
	job := pool.pending[address]
	if job.claimed.CompareAndSwap(false, true) {
		pool.execute(job)
		return
	}
	<-job.done
}

// finish stops the workers after all queued jobs complete and returns the first job error.
func (pool *nativeReconstructionPool) finish() error {
	if pool == nil {
		return nil
	}
	if pool.jobs != nil {
		close(pool.jobs)
		pool.jobs = nil
		pool.workers.Wait()
	}
	return pool.err
}

// deblockNativePictureThreads filters macroblock rows as a wavefront. A macroblock reads and
// writes at most four samples into its left and top neighbors, so it may run once the row
// above has finished the macroblock to its upper right; the result matches raster order.
func deblockNativePictureThreads(frame *Yuv420Frame, blocks []nativeMacroblock, threads int) error {
	width := frame.Width / 16
	if width == 0 || len(blocks)%width != 0 {
		return deblockNativePicture(frame, blocks)
	}
	rows := len(blocks) / width
	threads = min(threads, rows)
	if threads <= 1 {
		return deblockNativePicture(frame, blocks)
	}
	progress := make([]atomic.Int64, rows)
	var failed atomic.Bool
	var group sync.WaitGroup
	var mutex sync.Mutex
	var firstErr error
	group.Add(threads)
	for worker := 0; worker < threads; worker++ {
		go func(worker int) {
			defer group.Done()
			for row := worker; row < rows; row += threads {
				for column := 0; column < width; column++ {
					if row > 0 {
						required := int64(min(column+2, width))
						for progress[row-1].Load() < required {
							if failed.Load() {
								return
							}
							runtime.Gosched()
						}
					}
					if err := deblockNativeMacroblock(frame, blocks, row*width+column); err != nil {
						mutex.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mutex.Unlock()
						failed.Store(true)
						return
					}
					progress[row].Store(int64(column + 1))
				}
			}
		}(worker)
	}
	group.Wait()
	return firstErr
}
