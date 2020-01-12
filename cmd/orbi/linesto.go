package main

import (
	"bufio"
	"io/ioutil"
	"os"
	"strings"
)

type Linesto struct {
	History []string
}

var ln = &Linesto{}

func MIRB_ADD_HISTORY(line string) {
  ln.History = append(ln.History, line)
}

func MIRB_LINE_FREE(line string) {
}

func MIRB_WRITE_HISTORY(path string) {
	buf := strings.Join(ln.History, "\n")
	_=ioutil.WriteFile(path, []byte(buf), 0640)
}

func MIRB_READ_HISTORY(path string) {
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		ln.History = []string{}
		return
	}

	ln.History = strings.Split(string(buf), "\n")
}

func MIRB_USING_HISTORY() {
	if len(ln.History) == 0 {
		ln.History = make([]string, 0, 100)
	}
}

func MIRB_READLINE(oneOrTwo bool, prompt1, propmpt2 string) string {
	// linenoise(ch)
	if oneOrTwo {
		print(prompt1)
	} else {
		print(propmpt2)
	}
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return line
}
