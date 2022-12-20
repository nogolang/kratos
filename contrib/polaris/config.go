package polaris

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/polarismesh/polaris-go"
	"github.com/polarismesh/polaris-go/pkg/model"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/log"
)

// ConfigOption is polaris config option.
type ConfigOption func(o *configOptions)

type configOptions struct {
	namespace  string
	fileGroup  string
	fileName   string
	configFile polaris.ConfigFile
}

// WithConfigNamespace with polaris config namespace
func WithConfigNamespace(namespace string) ConfigOption {
	return func(o *configOptions) {
		o.namespace = namespace
	}
}

// WithFileGroup with polaris config fileGroup
func WithFileGroup(fileGroup string) ConfigOption {
	return func(o *configOptions) {
		o.fileGroup = fileGroup
	}
}

// WithFileName with polaris config fileName
func WithFileName(fileName string) ConfigOption {
	return func(o *configOptions) {
		o.fileName = fileName
	}
}

type source struct {
	client  polaris.ConfigAPI
	options *configOptions
}

func (p *Polaris) Config(opts ...ConfigOption) (config.Source, error) {
	options := &configOptions{
		namespace: "default",
		fileGroup: "",
		fileName:  "",
	}

	for _, opt := range opts {
		opt(options)
	}

	if options.fileGroup == "" {
		return nil, errors.New("fileGroup invalid")
	}

	if options.fileName == "" {
		return nil, errors.New("fileName invalid")
	}

	return &source{
		client:  p.config,
		options: options,
	}, nil
}

// Load return the config values
func (s *source) Load() ([]*config.KeyValue, error) {
	configFile, err := s.client.GetConfigFile(s.options.namespace, s.options.fileGroup, s.options.fileName)
	if err != nil {
		fmt.Println("fail to get config.", err)
		return nil, err
	}

	if err != nil {
		return nil, err
	}
	content := configFile.GetContent()
	k := s.options.fileName

	s.options.configFile = configFile

	return []*config.KeyValue{
		{
			Key:    k,
			Value:  []byte(content),
			Format: strings.TrimPrefix(filepath.Ext(k), "."),
		},
	}, nil
}

// Watch return the watcher
func (s *source) Watch() (config.Watcher, error) {
	return newConfigWatcher(s.options.configFile), nil
}

type ConfigWatcher struct {
	configFile polaris.ConfigFile
	fullPath   string
}

type eventChan struct {
	closed bool
	event  chan model.ConfigFileChangeEvent
}

var eventChanMap = make(map[string]eventChan)

func getFullPath(namespace string, fileGroup string, fileName string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, fileGroup, fileName)
}

func receive(event model.ConfigFileChangeEvent) {
	meta := event.ConfigFileMetadata
	ec := eventChanMap[getFullPath(meta.GetNamespace(), meta.GetFileGroup(), meta.GetFileName())]
	defer func() {
		if err := recover(); err != nil {
			log.Error(err)
		}
	}()
	if !ec.closed {
		ec.event <- event
	}
}

func newConfigWatcher(configFile polaris.ConfigFile) *ConfigWatcher {
	configFile.AddChangeListener(receive)

	fullPath := getFullPath(configFile.GetNamespace(), configFile.GetFileGroup(), configFile.GetFileName())
	if _, ok := eventChanMap[fullPath]; !ok {
		eventChanMap[fullPath] = eventChan{
			closed: false,
			event:  make(chan model.ConfigFileChangeEvent),
		}
	}
	w := &ConfigWatcher{
		configFile: configFile,
		fullPath:   fullPath,
	}
	return w
}

func (w *ConfigWatcher) Next() ([]*config.KeyValue, error) {
	ec := eventChanMap[w.fullPath]
	event := <-ec.event
	return []*config.KeyValue{
		{
			Key:    w.configFile.GetFileName(),
			Value:  []byte(event.NewValue),
			Format: strings.TrimPrefix(filepath.Ext(w.configFile.GetFileName()), "."),
		},
	}, nil
}

func (w *ConfigWatcher) Stop() error {
	ec := eventChanMap[w.fullPath]
	if !ec.closed {
		ec.closed = true
		close(ec.event)
	}
	return nil
}