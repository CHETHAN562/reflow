package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
)

var Log = logrus.New()

func InitLogger(debug bool) {
	Log.SetOutput(os.Stdout)
	if debug {
		Log.SetLevel(logrus.DebugLevel)
		Log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
			ForceColors:   true,
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				s := strings.Split(f.Function, ".")
				funcname := s[len(s)-1]
				filename := filepath.Base(f.File)
				return funcname, " [" + filename + ":" + string(rune(f.Line)) + "]"
			},
		})
		Log.SetReportCaller(true)
		Log.Debug("Debug logging enabled")
	} else {
		Log.SetLevel(logrus.InfoLevel)
		Log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
			ForceColors:   true,
		})
	}
}
