package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/Miku0139oao/aster-core/common/pool"
	"github.com/Miku0139oao/aster-core/common/yaml"
	"github.com/Miku0139oao/aster-core/component/resource"
	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
	"github.com/Miku0139oao/aster-core/rules/common"
)

var tunnel P.Tunnel

func SetTunnel(t P.Tunnel) {
	tunnel = t
}

type RulePayload struct {
	/**
	key: Domain or IP Cidr
	value: Rule type or is empty
	*/
	Payload []string `yaml:"payload"`
	Rules   []string `yaml:"rules"`
}

type providerForApi struct {
	Behavior    string    `json:"behavior"`
	Format      string    `json:"format"`
	Name        string    `json:"name"`
	RuleCount   int       `json:"ruleCount"`
	Type        string    `json:"type"`
	VehicleType string    `json:"vehicleType"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Payload     []string  `json:"payload,omitempty"`
}

type ruleStrategy interface {
	Behavior() P.RuleBehavior
	Match(metadata *C.Metadata, helper C.RuleMatchHelper) bool
	Count() int
	Reset()
	Insert(rule string)
	FinishInsert()
}

type mrsRuleStrategy interface {
	ruleStrategy
	FromMrs(r io.Reader, count int) error
	WriteMrs(w io.Writer) error
	DumpMrs(f func(key string) bool)
}

type strategySnapshot struct {
	strategy ruleStrategy
}

type baseProvider struct {
	behavior P.RuleBehavior
	strategy atomic.Pointer[strategySnapshot]
}

func (bp *baseProvider) setStrategy(strategy ruleStrategy) {
	bp.strategy.Store(&strategySnapshot{strategy: strategy})
}

func (bp *baseProvider) loadStrategy() ruleStrategy {
	snapshot := bp.strategy.Load()
	if snapshot == nil {
		return nil
	}
	return snapshot.strategy
}

func (bp *baseProvider) Type() P.ProviderType {
	return P.Rule
}

func (bp *baseProvider) Behavior() P.RuleBehavior {
	return bp.behavior
}

func (bp *baseProvider) Count() int {
	strategy := bp.loadStrategy()
	if strategy == nil {
		return 0
	}
	return strategy.Count()
}

func (bp *baseProvider) Match(metadata *C.Metadata, helper C.RuleMatchHelper) bool {
	strategy := bp.loadStrategy()
	return strategy != nil && strategy.Match(metadata, helper)
}

func (bp *baseProvider) Strategy() any {
	return bp.loadStrategy()
}

type ruleSetProvider struct {
	baseProvider
	*resource.Fetcher[ruleStrategy]
	format P.RuleFormat
}

type RuleSetProvider struct {
	*ruleSetProvider
}

func (rp *ruleSetProvider) Initial() error {
	_, err := rp.Fetcher.Initial()
	return err
}

func (rp *ruleSetProvider) Update() error {
	_, _, err := rp.Fetcher.Update()
	return err
}

func (rp *ruleSetProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(
		providerForApi{
			Behavior:    rp.behavior.String(),
			Format:      rp.format.String(),
			Name:        rp.Fetcher.Name(),
			RuleCount:   rp.Count(),
			Type:        rp.Type().String(),
			UpdatedAt:   rp.UpdatedAt(),
			VehicleType: rp.VehicleType().String(),
		})
}

func (rp *RuleSetProvider) Close() error {
	runtime.SetFinalizer(rp, nil)
	return rp.ruleSetProvider.Close()
}

func NewRuleSetProvider(name string, behavior P.RuleBehavior, format P.RuleFormat, interval time.Duration, vehicle P.Vehicle, payload []string, bundleFile resource.BundleFile, parse common.ParseRuleFunc) P.RuleProvider {
	rp := &ruleSetProvider{
		baseProvider: baseProvider{
			behavior: behavior,
		},
		format: format,
	}

	onUpdate := func(strategy ruleStrategy) {
		rp.setStrategy(strategy)
		tunnel.RuleUpdateCallback().Emit(rp)
	}

	strategy := newStrategy(behavior, parse)
	if len(payload) > 0 { // using as fallback rules
		strategy = rulesParseInline(payload, strategy)
	}
	rp.setStrategy(strategy)
	rp.Fetcher = resource.NewFetcher(name, interval, vehicle, bundleFile, func(bytes []byte) (ruleStrategy, error) {
		return rulesParse(bytes, newStrategy(behavior, parse), format)
	}, onUpdate)

	wrapper := &RuleSetProvider{
		rp,
	}

	runtime.SetFinalizer(wrapper, (*RuleSetProvider).Close)
	return wrapper
}

func newStrategy(behavior P.RuleBehavior, parse common.ParseRuleFunc) ruleStrategy {
	switch behavior {
	case P.Domain:
		strategy := NewDomainStrategy()
		return strategy
	case P.IPCIDR:
		strategy := NewIPCidrStrategy()
		return strategy
	case P.Classical:
		strategy := NewClassicalStrategy(parse)
		return strategy
	default:
		return nil
	}
}

var (
	ErrNoPayload     = errors.New("file must have a `payload` field")
	ErrInvalidFormat = errors.New("invalid format")
)

func rulesParse(buf []byte, strategy ruleStrategy, format P.RuleFormat) (ruleStrategy, error) {
	strategy.Reset()
	switch format {
	case P.MrsRule:
		return rulesMrsParse(buf, strategy)
	case P.YamlRule:
		return rulesParseYAML(buf, strategy)
	case P.TextRule:
		return rulesParseText(buf, strategy)
	default:
		return nil, ErrInvalidFormat
	}
}

func insertParsedRules(strategy ruleStrategy, rules []string) {
	for _, rule := range rules {
		if rule != "" {
			strategy.Insert(rule)
		}
	}
	strategy.FinishInsert()
}

func rulesParseYAML(buf []byte, strategy ruleStrategy) (ruleStrategy, error) {
	schema := &RulePayload{}
	if err := yaml.Unmarshal(bytes.TrimSpace(buf), schema); err == nil {
		// Files with both payload and rules used to be scraped line-by-line
		// and could ingest both sections. Keep that fallback path.
		if len(schema.Payload) > 0 && len(schema.Rules) > 0 {
			return rulesParseYAMLLines(buf, strategy)
		}
		rules := schema.Payload
		if len(rules) == 0 {
			rules = schema.Rules
		}
		if len(rules) == 0 {
			if bytes.IndexByte(buf, '\n') < 0 {
				return nil, ErrNoPayload
			}
			// Empty lists used to succeed for a payload: head with no body.
			return rulesParseYAMLLines(buf, strategy)
		}
		insertParsedRules(strategy, rules)
		return strategy, nil
	}
	if bytes.IndexByte(buf, '\n') < 0 {
		return nil, ErrNoPayload
	}
	return rulesParseYAMLLines(buf, strategy)
}

func rulesParseText(buf []byte, strategy ruleStrategy) (ruleStrategy, error) {
	s := 0
	for s < len(buf) {
		line := buf[s:]
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
			s += i + 1
		} else {
			s = len(buf)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if len(line) >= 2 && line[0] == '/' && line[1] == '/' {
			continue
		}
		strategy.Insert(string(line))
	}
	strategy.FinishInsert()
	return strategy, nil
}

func rulesParseYAMLLines(buf []byte, strategy ruleStrategy) (ruleStrategy, error) {
	schema := &RulePayload{}
	firstLineBuffer := pool.GetBuffer()
	defer pool.PutBuffer(firstLineBuffer)
	firstLineLength := 0

	s := 0 // search start index
	for s < len(buf) {
		// search buffer for a new line.
		line := buf[s:]
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			i += s
			line = buf[s : i+1]
			s = i + 1
		} else {
			s = len(buf)              // stop loop in next step
			if firstLineLength == 0 { // no head or only one line body
				return nil, ErrNoPayload
			}
		}
		trimLine := bytes.TrimSpace(line)
		if len(trimLine) == 0 {
			continue
		}
		if trimLine[0] == '#' { // comment
			continue
		}
		firstLineBuffer.Write(line)
		if firstLineLength == 0 { // find payload head
			firstLineLength = firstLineBuffer.Len()
			firstLineBuffer.WriteString("  - ''") // a test line

			err := yaml.Unmarshal(firstLineBuffer.Bytes(), schema)
			firstLineBuffer.Truncate(firstLineLength)
			if err == nil && (len(schema.Rules) > 0 || len(schema.Payload) > 0) { // found
				continue
			}

			// not found or err!=nil
			firstLineBuffer.Truncate(0)
			firstLineLength = 0
			continue
		}

		// parse payload body
		err := yaml.Unmarshal(firstLineBuffer.Bytes(), schema)
		firstLineBuffer.Truncate(firstLineLength)
		if err != nil {
			continue
		}

		var str string
		if len(schema.Rules) > 0 {
			str = schema.Rules[0]
		}
		if len(schema.Payload) > 0 {
			str = schema.Payload[0]
		}
		if str == "" {
			continue
		}
		strategy.Insert(str)
	}

	strategy.FinishInsert()
	return strategy, nil
}

func rulesParseInline(rs []string, strategy ruleStrategy) ruleStrategy {
	strategy.Reset()
	for _, r := range rs {
		if r != "" {
			strategy.Insert(r)
		}
	}
	strategy.FinishInsert()
	return strategy
}

type InlineProvider struct {
	*inlineProvider
}

type inlineProvider struct {
	baseProvider
	name     string
	updateAt atomic.Int64
	payload  []string
}

func (i *inlineProvider) Name() string {
	return i.name
}

func (i *inlineProvider) Initial() error {
	return nil
}

func (i *inlineProvider) Update() error {
	// make api update happy
	i.updateAt.Store(time.Now().UnixNano())
	return nil
}

func (i *inlineProvider) VehicleType() P.VehicleType {
	return P.Inline
}

func (i *inlineProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(
		providerForApi{
			Behavior:    i.behavior.String(),
			Name:        i.Name(),
			RuleCount:   i.Count(),
			Type:        i.Type().String(),
			VehicleType: i.VehicleType().String(),
			UpdatedAt:   time.Unix(0, i.updateAt.Load()),
			Payload:     i.payload,
		})
}

func NewInlineProvider(name string, behavior P.RuleBehavior, payload []string, parse common.ParseRuleFunc) P.RuleProvider {
	strategy := newStrategy(behavior, parse)
	strategy = rulesParseInline(payload, strategy)
	ip := &inlineProvider{
		baseProvider: baseProvider{behavior: behavior},
		payload:      payload,
		name:         name,
	}
	ip.setStrategy(strategy)
	ip.updateAt.Store(time.Now().UnixNano())

	wrapper := &InlineProvider{
		ip,
	}

	// runtime.SetFinalizer(wrapper, (*InlineProvider).Close)
	return wrapper
}
