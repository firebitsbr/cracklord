package hashcatdict

import (
	"bytes"
	"errors"
	"github.com/jmmcatee/cracklord/common"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var regLastStatusIndex *regexp.Regexp
var regStatus *regexp.Regexp
var regRuleType *regexp.Regexp
var regInputMode *regexp.Regexp
var regHashTarget *regexp.Regexp
var regHashType *regexp.Regexp
var regTimeStarted *regexp.Regexp
var regTimeEstimated *regexp.Regexp
var regGPUSpeed *regexp.Regexp
var regRecovered *regexp.Regexp
var regProgress *regexp.Regexp
var regRejected *regexp.Regexp
var regGPUHWMon *regexp.Regexp

var regGetGPUCount *regexp.Regexp
var regGetNumerator *regexp.Regexp
var regGetDenominator *regexp.Regexp
var regGetPercent *regexp.Regexp

func init() {
	var err error
	regLastStatusIndex, err = regexp.Compile(`Session\.Name\.\.\.\:\s+oclHashcat`)
	regStatus, err = regexp.Compile(`(Status)\.\.\.\.\.\.\.\.\.\:\s+(\w+)`)
	regRuleType, err = regexp.Compile(`(Rules\.Type)\.\.\.\.\.\:\s+(\w+)\s+\((.+)\)`)
	regInputMode, err = regexp.Compile(`(Input\.Mode)\.\.\.\.\.\:\s+(\w+)\s+\((.+)\)`)
	regHashTarget, err = regexp.Compile(`(Hash\.Target)\.\.\.\.\:\s+([0-9a-fA-F]+)`)
	regHashType, err = regexp.Compile(`(Hash\.Type)\.\.\.\.\.\.\:\s+(\w+)`)
	regTimeStarted, err = regexp.Compile(`(Time\.Started)\.\.\.\:\s+(.+\(.+\))`)
	regTimeEstimated, err = regexp.Compile(`(Time\.Estimated)\.\:\s+(.+\(.+\))`)
	regGPUSpeed, err = regexp.Compile(`(Speed\.GPU\.#.+)\.\.\.\:\s+(.+)`)
	regRecovered, err = regexp.Compile(`(Recovered)\.\.\.\.\.\.\:\s+0\/1\s+(.+)`)
	regProgress, err = regexp.Compile(`(Progress)\.\.\.\.\.\.\.\:\s+(\d+\/\d+.+)`)
	regRejected, err = regexp.Compile(`(Rejected)\.\.\.\.\.\.\.\:\s+(\d+\/\d+.+)`)
	regGPUHWMon, err = regexp.Compile(`(HWMon\.GPU\.#\d+)\.\.\.\:\s+(.+)`)

	regGetGPUCount, err = regexp.Compile(`\#(\d)`)
	regGetNumerator, err = regexp.Compile(`(\d+\)/\d+`)
	regGetDenominator, err = regexp.Compile(`(d+\/(\d+)`)
	regGetPercent, err = regexp.Compile(`\(\d+\.\d+\%\)`)

	if err != nil {
		panic(err.Error())
	}
}

type hascatTasker struct {
	job        common.Job
	wd         string
	cmd        exec.Cmd
	start      []string
	resume     []string
	stderr     *bytes.Buffer
	stdout     *bytes.Buffer
	stderrPipe io.ReadCloser
	stdoutPipe io.ReadCloser
	stdinPipe  io.WriteCloser
	done       chan bool
}

func newHashcatTask(j common.Job) (common.Tasker, error) {
	h := hascatTasker{}

	h.job = j

	// Build a working directory for this job
	h.wd = filepath.Join(config.WorkDir, h.job.UUID)
	err := os.Mkdir(h.wd, 700)
	if err != nil {
		// Couldn't make a directory so kill the job
		return &hascatTasker{}, errors.New("Could not create a working directory.")
	}

	// Build the arguements for hashcat
	args := []string{}

	// Get the hash type and add an argument
	htype, ok := config.HashTypes[h.job.Parameters["algorithm"]]
	if !ok {
		return &hascatTasker{}, errors.New("Could not find the algorithm provided.")
	}

	args = append(args, "-m", htype)

	// Add the rule file to use if one was given
	ruleKey, ok := h.job.Parameters["rules"]
	if ok {
		// We have a rule file, check for blank
		if ruleKey != "" {
			rulePath, ok := config.Rules[ruleKey]
			if ok {
				args = append(args, "-r", rulePath)
			}
		}
	}

	args = append(args, "--force")

	// Add an output file
	args = append(args, "-o", filepath.Join(h.wd, "hashes-output.txt"))

	// Take the hashes given and create a file
	hashFile, err := os.Create(filepath.Join(h.wd, "hashes.txt"))
	if err != nil {
		return &hascatTasker{}, err
	}

	hashFile.WriteString(h.job.Parameters["hashes"])

	// Append that file to the arguments
	args = append(args, filepath.Join(h.wd, "hashes.txt"))

	// Check for dictionary given
	dictKey, ok := h.job.Parameters["dictionaries"]
	if !ok {
		return &hascatTasker{}, errors.New("No dictionary provided.")
	}

	dictPath, ok := config.Dictionaries[dictKey]
	if !ok {
		return &hascatTasker{}, errors.New("Dictionary key provided was not present.")
	}

	// Add dictionary to arguments
	args = append(args, dictPath)

	log.Printf("Arguments: %v\n", args)

	// Get everything except the session identifier because the Resume command will be different
	h.start = append(h.start, "--session="+h.job.UUID)
	h.resume = append(h.resume, "--session="+h.job.UUID)
	h.resume = append(h.resume, "--restore")

	h.start = append(h.start, args...)
	h.resume = append(h.resume, args...)

	// Configure the return values
	h.job.PerformanceTitle = "MH/sec"
	h.job.OutputTitles = []string{"Hash", "Plaintext"}

	return &h, nil
}

func (v *hascatTasker) Status() common.Job {
	var err error

	// Update job internals
	io.WriteString(v.stdinPipe, "s")

	// Wait a few microseconds
	<-time.After(10 * time.Microsecond)

	status := v.stdout.String()

	statFound := regStatus.FindAllString(status, -1)
	log.Println(statFound)
	// if len(statFound) != 0 {
	// 	v.job.Output["Status"] = statFound[len(statFound)-1]
	// }

	progFound := regProgress.FindAllString(status, -1)
	log.Println(progFound)

	progRecovered := regRecovered.FindAllString(status, -1)
	log.Println(progRecovered)

	// v.job.CrackedHashes, err = strconv.ParseInt(regGetNumerator.FindString(progRecovered[len(progRecovered)-1]), 0, 64)
	// if err != nil {
	// 	v.job.Output["Errors"] += ";" + err.Error()
	// }

	// v.job.TotalHashes, err = strconv.ParseInt(regGetDenominator.FindString(progRecovered[len(progRecovered)-1]), 0, 64)
	// if err != nil {
	// 	v.job.Output["Errors"] += ";" + err.Error()
	// }

	progRejected := regRejected.FindAllString(status, -1)
	log.Println(progRejected)

	progHashTarget := regHashTarget.FindAllString(status, -1)
	log.Println(progHashTarget)

	progHashType := regHashType.FindAllString(status, -1)
	log.Println(progHashType)

	progInputMode := regInputMode.FindAllString(status, -1)
	log.Println(progInputMode)

	progRuleType := regRuleType.FindAllString(status, -1)
	log.Println(progRuleType)

	progTimeEst := regTimeEstimated.FindAllString(status, -1)
	log.Println(progTimeEst)

	progTimeStart := regTimeStarted.FindAllString(status, -1)
	log.Println(progTimeStart)

	// progGPUHWMon := regGPUHWMon.FindAllString(status, -1)
	// if len(progGPUHWMon) != 0 {
	// 	numGPUs := regGetGPUCount.FindString(progGPUHWMon[len(progGPUHWMon)-1])
	// 	numGPUsInt, err := strconv.Atoi(numGPUs)
	// 	if err == nil {
	// 		for i := numGPUsInt; i > 0; i-- {
	// 			s := strconv.Itoa(i)
	// 			x := numGPUsInt - 1
	// 			v.job.Output["HWMon.GPU.#"+s] = progGPUHWMon[len(progGPUHWMon)-(x-i)]
	// 		}
	// 	}
	// }

	// progGPUSpeed := regGPUSpeed.FindAllString(status, -1)
	// if len(progGPUSpeed) != 0 {
	// 	numGPUs := regGetGPUCount.FindString(progGPUSpeed[len(progGPUSpeed)-1])
	// 	numGPUsInt, err := strconv.Atoi(numGPUs)
	// 	if err == nil {
	// 		for i := numGPUsInt; i > 0; i-- {
	// 			s := strconv.Itoa(i)
	// 			x := numGPUsInt - 1
	// 			v.job.Output["HWMon.GPU.#"+s] = progGPUSpeed[len(progGPUSpeed)-(x-i)]
	// 		}
	// 	}
	// }

	// Get the output results
	file, err := ioutil.ReadFile(filepath.Join(v.wd, "hashes-output.txt"))
	if err != nil {
		log.Println(err.Error())
	} else {
		content := strings.Split(string(file), ":")
		if len(content) > 1 {
			for _, s := range content[1:] {
				v.job.OutputData[content[0]] += s
			}
		}
	}

	// Check if we are done
	done := false
	select {
	case <-v.done:
		done = true
	default:
	}

	// Run finished script
	if done {
		v.job.Status = common.STATUS_DONE

		// wait for file to finish writing possible
		<-time.After(10 * time.Millisecond)
		file, err := ioutil.ReadFile(filepath.Join(v.wd, "hashes-output.txt"))
		log.Println(string(file))
		if err != nil {
			log.Println(err.Error())
		} else {
			content := strings.Split(string(file), ":")
			if len(content) > 1 {
				for _, s := range content[1:] {
					v.job.OutputData[content[0]] += s
				}
			}
		}

		return v.job
	}

	log.Printf("Job: %+v\n", v.job)

	return v.job
}

func (v *hascatTasker) Run() error {
	// Check that we have not already finished this job
	done := v.job.Status == common.STATUS_DONE || v.job.Status == common.STATUS_QUIT || v.job.Status == common.STATUS_FAILED
	if done {
		return errors.New("Job already finished.")
	}

	// Check if this job is running
	if v.job.Status == common.STATUS_RUNNING {
		// Job already running so return no errors
		return nil
	}

	// Assign the stderr, stdout, stdin pipes
	var err error
	v.stderrPipe, err = v.cmd.StderrPipe()
	v.stdoutPipe, err = v.cmd.StdoutPipe()
	v.stdinPipe, err = v.cmd.StdinPipe()
	if err != nil {
		return err
	}

	v.stderr = bytes.NewBuffer([]byte{})
	v.stdout = bytes.NewBuffer([]byte{})

	go func() {
		for {
			io.Copy(v.stderr, v.stderrPipe)
		}
	}()
	go func() {
		for {
			io.Copy(v.stdout, v.stdoutPipe)
		}
	}()

	// Set commands for restore or start
	if v.job.Status == common.STATUS_CREATED {
		v.cmd = *exec.Command(config.BinPath, v.start...)
	} else {
		v.cmd = *exec.Command(config.BinPath, v.resume...)
	}

	v.cmd.Dir = v.wd

	// Start the command
	err = v.cmd.Start()
	v.job.StartTime = time.Now()
	if err != nil {
		// We had an error starting to return that and quit the job
		v.job.Status = common.STATUS_FAILED
		return err
	}

	v.job.Status = common.STATUS_RUNNING

	// Build goroutine to alert that the job has finished
	v.done = make(chan bool)
	go func() {
		// Listen on commmand wait and then send signal when finished
		// This will be read on the Status() function
		v.cmd.Wait()
		v.done <- true
	}()

	return nil
}

// Pause the hashcat run
func (v *hascatTasker) Pause() error {
	// Call status to update the job internals before pausing
	v.Status()

	// Because this is queue managed, we should just need to kill the process.
	// It will be resumed automatically
	_, err := io.WriteString(v.stdinPipe, "q")
	if err != nil {
		return err
	}

	// Change status to pause
	v.job.Status = common.STATUS_PAUSED

	return nil
}

func (v *hascatTasker) Quit() common.Job {
	// Call status to update the job internals before quiting
	v.Status()

	io.WriteString(v.stdinPipe, "q")

	v.job.Status = common.STATUS_QUIT

	return v.job
}

func (v *hascatTasker) IOE() (io.Writer, io.Reader, io.Reader) {
	return v.stdinPipe, v.stdoutPipe, v.stderrPipe
}