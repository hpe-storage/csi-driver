package mpathconfig

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"bufio"
	"container/list"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/hpe-storage/common-host-libs/logger"
	"github.com/hpe-storage/common-host-libs/util"
)

const (
	// NEWLINE indicates new line character
	NEWLINE = "\n"
	// MPATHCONF indicates configuration file path for multipathd
	MPATHCONF = "/etc/multipath.conf"
	// SectionNotFoundError represents error when section with given name is not found in config
	SectionNotFoundError = "unable to find section"
	// NimbleBackupSuffix common suffix for all file backups
	NimbleBackupSuffix = "nimblebackup"
)

var (
	propertyPattern = "^[\\s\\t]*(?P<param>[^\\s\\t]+)[\\s\\t]*(?P<value>\".+\"|[^\\s\\t]+)\\s*"
)

// Configuration represents entire multipath.conf along with all sections
type Configuration struct {
	file  string
	root  *Section
	mutex sync.RWMutex
}

// Section represents a multipath.conf section embedded between { and }
type Section struct {
	name       string
	parent     *Section
	properties map[string]string
	children   *list.List
	mutex      sync.RWMutex
	duplicates *list.List
}

// Duplicate manages duplicate params with same key in defaults section
type Duplicate struct {
	key   string
	value string
}

// GetRoot returns the root section
func (config *Configuration) GetRoot() *Section {
	return config.root
}

// GetName returns the name of the section
func (section *Section) GetName() string {
	section.mutex.RLock()
	defer section.mutex.RUnlock()

	return section.name
}

// SetParent sets parent of section
func (section *Section) SetParent(parent *Section) {
	section.mutex.Lock()
	defer section.mutex.Unlock()

	section.parent = parent
}

// GetParent returns the parent section
func (section *Section) GetParent() *Section {
	section.mutex.RLock()
	defer section.mutex.RUnlock()

	return section.parent
}

// GetChildren returns the list of child sections
func (section *Section) GetChildren() *list.List {
	section.mutex.RLock()
	defer section.mutex.RUnlock()

	return section.children
}

// GetProperties returns the key value properties in current section
func (section *Section) GetProperties() map[string]string {
	section.mutex.RLock()
	defer section.mutex.RUnlock()

	return section.properties
}

// PrintSection returns each section in string format
func (section *Section) PrintSection(indent int) (conf []string) {
	section.mutex.RLock()
	defer section.mutex.RUnlock()

	indenter := ""
	if indent > 0 {
		indenter = fmt.Sprintf("%"+strconv.Itoa(indent)+"s", " ")
	}

	// Begin section
	conf = append(conf, fmt.Sprintf("%s%s%s%s", indenter, section.name, " {", NEWLINE))
	var formattedProperty string
	spacerSize := 1
	for property := range section.properties {
		if len(property) > spacerSize {
			spacerSize = len(property)
		}
	}
	spacerSize++
	for property, value := range section.properties {
		formattedProperty = fmt.Sprintf("%s    %-"+strconv.Itoa(spacerSize)+"s%s%s", indenter, property, value, NEWLINE)
		conf = append(conf, formattedProperty)
	}
	// handle duplicate param case in defaults/alias/multipaths section
	if section.duplicates.Len() != 0 {
		for e := section.duplicates.Front(); e != nil; e = e.Next() {
			dup := e.Value.(*Duplicate)
			formattedProperty = fmt.Sprintf("%s    %-"+strconv.Itoa(spacerSize)+"s%s%s", indenter, dup.key, dup.value, NEWLINE)
			conf = append(conf, formattedProperty)
		}
	}
	if section.children.Len() != 0 {
		indent = indent + 4
		for e := section.children.Front(); e != nil; e = e.Next() {
			s := e.Value.(*Section)
			sectionParams := s.PrintSection(indent)
			for _, line := range sectionParams {
				conf = append(conf, line)
			}
		}
	}

	// End section
	conf = append(conf, fmt.Sprintf("%s%s%s", indenter, "}", NEWLINE))
	return conf
}

// PrintConf returns the string representation of current section and all child sections
func (config *Configuration) PrintConf() (conf []string) {
	log.Trace("PrintConf called")
	config.mutex.RLock()
	defer config.mutex.RUnlock()

	root := config.GetRoot()
	if root != nil && root.GetChildren().Len() != 0 {
		for e := root.GetChildren().Front(); e != nil; e = e.Next() {
			s := e.Value.(*Section)
			sectionConf := s.PrintSection(0)
			for _, line := range sectionConf {
				conf = append(conf, line)
			}
		}
	}
	return conf
}

// newConfiguration creates a new Configuration instance.
func newConfiguration(filePath string) *Configuration {
	return &Configuration{
		file: filePath,
		root: &Section{name: "root", properties: nil, parent: nil, children: list.New(), duplicates: list.New()},
	}
}

// AddSection adds a new section into config
func (config *Configuration) AddSection(sectionName string, parent *Section) (section *Section, err error) {
	log.Trace("addSection called with ", sectionName)
	section = &Section{name: sectionName, properties: make(map[string]string), parent: parent, children: list.New(), duplicates: list.New()}
	foundParent := false

	config.mutex.Lock()
	defer config.mutex.Unlock()

	root := config.GetRoot()
	if root != nil {
		if parent.GetName() == root.GetName() {
			// add new section under root
			root.GetChildren().PushBack(section)
			foundParent = true
		} else {
			for e := root.GetChildren().Front(); e != nil; e = e.Next() {
				// find parent to insert the section
				if e.Value.(*Section).GetName() == parent.GetName() {
					e.Value.(*Section).GetChildren().PushBack(section)
					foundParent = true
				}
			}
		}
	}
	if !foundParent {
		return nil, errors.New("unable to find parent to insert section " + sectionName)
	}
	return section, nil
}

func parseOption(option string) (opt, value string) {
	log.Trace("parseOption called")
	r := regexp.MustCompile(propertyPattern)
	if r.MatchString(option) {
		result := util.FindStringSubmatchMap(option, r)
		opt := result["param"]
		value := result["value"]
		log.Trace("parseOption param ", opt, " value ", value)
		return strings.TrimSpace(opt), strings.TrimSpace(value)
	}
	return
}

func addOption(section *Section, option string) {
	section.mutex.Lock()
	defer section.mutex.Unlock()

	var key, value string
	if section != nil {
		if key, value = parseOption(option); value != "" {
			if _, ok := section.properties[key]; ok {
				// already another parameter present in section with same name, add to duplicates
				duplicate := &Duplicate{key: key, value: value}
				section.duplicates.PushBack(duplicate)
			} else {
				section.properties[key] = value
			}
		}
	}
}

//checks for the section
func isSection(line string) (bool, error) {
	line = strings.TrimSpace(line)
	prefixes := []string{"defaults", "blacklist", "blacklist_exceptions", "devices", "device", "multipaths", "multipath"}
	for _, prefix := range prefixes {
		r, err := regexp.Compile("^" + prefix + "\\s*[{]*$")
		if err != nil {
			return false, err
		}

		if r.MatchString(line) {
			return true, nil
		}
	}

	return false, nil
}

// ParseConfig reads and parses give config file into sections
func ParseConfig(filePath string) (config *Configuration, err error) {
	log.Trace("ParseConfig called")
	filePath = path.Clean(filePath)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// initialize new configuration
	config = newConfiguration(filePath)
	// initialized to root section
	currentSection := config.root

	scanner := bufio.NewScanner(bufio.NewReader(file))
	for scanner.Scan() {
		line := scanner.Text()
		if !(strings.HasPrefix(line, "#")) && len(line) > 0 {
			section_present, err := isSection(line)
			if err != nil {
				return nil, err
			}
			if section_present {
				name := strings.Trim(line, " {")
				// add new section with parent updated
				log.Trace("adding section ", name)
				currentSection, err = config.AddSection(name, currentSection)
				if err != nil {
					return nil, err
				}
				// indicate new section begun
				continue
			} else {
				if strings.TrimSpace(line) == "{" {
					// ignore beginning of section if { is in different line than section name
					continue
				} else if strings.TrimSpace(line) == "}" {
					// end section
					currentSection = currentSection.parent
					continue
				}
				addOption(currentSection, line)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return config, nil
}

// SaveConfig writes corrected config to file after taking backup
func SaveConfig(config *Configuration, filePath string) (err error) {
	log.Trace("SaveConfig called")

	config.mutex.Lock()
	err = TakeBackupOfConfFile(MPATHCONF, NimbleBackupSuffix)
	if err != nil {
		return err
	}

	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() {
		err = f.Close()
	}()

	w := bufio.NewWriter(f)
	defer func() {
		err = w.Flush()
	}()
	config.mutex.Unlock()

	s := config.PrintConf()

	config.mutex.Lock()
	defer config.mutex.Unlock()
	for _, line := range s {
		w.WriteString(line)
	}

	return err
}

// GetDeviceSection gets device section in /etc/multipath.conf
func (config *Configuration) GetDeviceSection(deviceType string) (section *Section, err error) {
	log.Trace("GetDeviceSection called for device: ", deviceType)
	config.mutex.RLock()
	defer config.mutex.RUnlock()

	root := config.GetRoot()
	if root != nil {
		for e := root.children.Front(); e != nil; e = e.Next() {
			s := e.Value.(*Section)
			if s.GetName() == "devices" {
				if s.GetChildren().Len() != 0 {
					for e := s.GetChildren().Front(); e != nil; e = e.Next() {
						childSection := e.Value.(*Section)
						if childSection.GetName() == "device" && strings.Contains(childSection.properties["vendor"], deviceType) {
							return childSection, nil
						}
					}
				}
			}
		}
	}
	return nil, errors.New("nimble section is not found")
}

// GetSection returns section with matching name
func (config *Configuration) GetSection(sectionName, vendor string) (section *Section, err error) {
	log.Trace("GetSection called with ", sectionName)
	config.mutex.RLock()
	defer config.mutex.RUnlock()

	root := config.GetRoot()
	if root != nil {
		for e := root.children.Front(); e != nil; e = e.Next() {
			s := e.Value.(*Section)
			if s.GetName() == sectionName {
				return s, nil
			} else if s.GetChildren().Len() != 0 {
				for e := s.GetChildren().Front(); e != nil; e = e.Next() {
					childSection := e.Value.(*Section)
					if childSection.GetName() == sectionName {
						// verify if device section is really under devices
						if childSection.GetName() == "device" && !strings.Contains(childSection.properties["vendor"], vendor) {
							continue
						}
						return childSection, nil
					}
				}
			}
		}
	}
	return nil, errors.New(SectionNotFoundError + " with name " + sectionName)
}

// TakeBackupOfConfFile take a backup of srcFile after taking backup in same directory with suffix and timestamp appended
func TakeBackupOfConfFile(srcFile string, backupSuffix string) (err error) {
	backupName := fmt.Sprintf("%s-%s-%s", srcFile, backupSuffix, time.Now().Format("2006-01-02 15:04:05"))
	err = util.CopyFile(srcFile, backupName)
	if err != nil {
		if !os.IsNotExist(err) { // fine if the file does not exists
			return err
		}
	}
	return nil
}

// IsUserFriendlyNamesEnabled returns true if user_friendly_names is enabled
func IsUserFriendlyNamesEnabled() (bool, error) {
	log.Tracef(">>> IsUserFriendlyNamesEnabled")
	defer log.Tracef("<<< IsUserFriendlyNamesEnabled")
	defaults, err := GetDefaultsSection()
	if err != nil && strings.Contains(err.Error(), SectionNotFoundError) {
		// if defaults{} section is not present, then user_friendly_names is disabled by default
		return false, nil
	} else if err != nil {
		log.Errorf("unable to parse defaults section from multipath.conf, err %s", err.Error())
		// defaults{} present, but couldn't parse, error out
		return false, err
	}
	defaultsMap := defaults.GetProperties()
	if val, ok := defaultsMap["user_friendly_names"]; ok && strings.Trim(val, "\"") == "yes" {
		// explicitly enabled by user
		return true, nil
	}
	return false, nil
}

// GetDefaultsSection returns defaults{} section from multipath.conf
func GetDefaultsSection() (defaults *Section, err error) {
	log.Tracef(">>> GetDefaultsSection")
	defer log.Tracef("<<< GetDefaultsSection")
	// parse multipath.conf into different sections
	config, err := ParseConfig(MPATHCONF)
	if err != nil {
		return nil, err
	}
	// update find_multipaths as no if set in defaults section
	defaults, err = config.GetSection("defaults", "")
	if err != nil {
		return nil, err
	}
	return defaults, nil
}
