# Logger, helper around logrus

A Helper around [Logrus](https://github.com/sirupsen/logrus) to provide additional functionality as below:

1. Initializer to set default log level, log location etc
2. Initializer to automatically add hooks for stdout and file
3. Supports log rotation with max_size and max_file params
4. Allow command line overrides using flag

## Howto use it

```go
import(
    log "github.com/hpe-storage/common-host-libs/logger"
)

// Example1:
// initialize with nil params to consider default values
func main() {
    log.InitLogging("/var/log/hpe-storage.log", nil, false)

    // ### Use logrus as normal
    log.WithFields(log.Fields{
    "provisioner": "doryd",
    }).Error("error appears here")
}

// Example2:
// initialize with overriding log_level to debug: default(info)
func main() {
    log.InitLogging("/var/log/hpe-storage.log", &log.LogParams{Level: "debug"}, false)
    // ### Use logrus as normal
    log.WithFields(log.Fields{
    "provisioner": "doryd",
    }).Debug("debug appears here")
}


// Example3:
// initialize with logging to both file and stdout/stderr
func main() {
    log.InitLogging("/var/log/hpe-storage.log", nil, true)
    // ### Use logrus as normal
    log.WithFields(log.Fields{
    "provisioner": "doryd",
    }).Error("error appears here")
}


// Example4:
// initialize with log format as json: default(text)
func main() {
    log.InitLogging("/var/log/hpe-storage.log", &log.LogParams{Format: "json"}, false)
    // ### Use logrus as normal
    log.WithFields(log.Fields{
    "provisioner": "doryd",
    }).Error("error appears here")
}

// Example5:
// initialize with log level as trace using env params
func main() {
    os.Setenv("LOG_LEVEL", "trace")
    log.InitLogging("/var/log/hpe-storage.log", nil, false)
    // ### Use logrus as normal
    log.WithFields(log.Fields{
    "provisioner": "doryd",
    }).Trace("trace appears here")
}

```
