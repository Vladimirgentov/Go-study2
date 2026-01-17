package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ValidationError struct {
	File string
	Line int
	Msg  string
}

func (e ValidationError) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d %s", e.File, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Msg)
}

func main() {
	if len(os.Args) != 2 {
		os.Exit(1)
	}

	filename := os.Args[1]
	b, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println(ValidationError{File: filename, Line: 0, Msg: "cannot read file content"})
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		fmt.Println(ValidationError{File: filename, Line: 0, Msg: "cannot unmarshal file content"})
		os.Exit(1)
	}

	errs := validatePodYAML(filename, &root)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Println(e.String())
		}
		os.Exit(1)
	}
}

func validatePodYAML(file string, root *yaml.Node) []ValidationError {
	var errs []ValidationError

	if root == nil || len(root.Content) == 0 {
		return []ValidationError{{File: file, Line: 0, Msg: "cannot unmarshal file content"}}
	}

	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}

	if n.Kind != yaml.MappingNode {
		return []ValidationError{{File: file, Line: 0, Msg: "cannot unmarshal file content"}}
	}

	m := n

	apiNode, ok := mapGet(m, "apiVersion")
	if !ok {
		errs = append(errs, req(file, "apiVersion"))
	} else if apiNode.Kind != yaml.ScalarNode {
		errs = append(errs, typeErr(file, apiNode.Line, "apiVersion", "string"))
	} else if strings.TrimSpace(apiNode.Value) != "v1" {
		errs = append(errs, unsupported(file, apiNode.Line, "apiVersion", apiNode.Value))
	}

	kindNode, ok := mapGet(m, "kind")
	if !ok {
		errs = append(errs, req(file, "kind"))
	} else if kindNode.Kind != yaml.ScalarNode {
		errs = append(errs, typeErr(file, kindNode.Line, "kind", "string"))
	} else if strings.TrimSpace(kindNode.Value) != "Pod" {
		errs = append(errs, unsupported(file, kindNode.Line, "kind", kindNode.Value))
	}

	metaNode, ok := mapGet(m, "metadata")
	if !ok {
		errs = append(errs, req(file, "metadata"))
	} else if metaNode.Kind != yaml.MappingNode {
		errs = append(errs, typeErr(file, metaNode.Line, "metadata", "object"))
	} else {
		errs = append(errs, validateObjectMeta(file, metaNode)...)
	}

	specNode, ok := mapGet(m, "spec")
	if !ok {
		errs = append(errs, req(file, "spec"))
	} else if specNode.Kind != yaml.MappingNode {
		errs = append(errs, typeErr(file, specNode.Line, "spec", "object"))
	} else {
		errs = append(errs, validatePodSpec(file, specNode)...)
	}

	return errs
}

func validateObjectMeta(file string, meta *yaml.Node) []ValidationError {
	var errs []ValidationError

	nameNode, ok := mapGet(meta, "name")
	if !ok {
		errs = append(errs, req(file, "metadata.name"))
	} else {
		if nameNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, nameNode.Line, "metadata.name", "string"))
		} else if strings.TrimSpace(nameNode.Value) == "" {
			errs = append(errs, invalidFormat(file, nameNode.Line, "metadata.name", nameNode.Value))
		}
	}

	if nsNode, ok := mapGet(meta, "namespace"); ok {
		if nsNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, nsNode.Line, "metadata.namespace", "string"))
		}
	}

	if labelsNode, ok := mapGet(meta, "labels"); ok {
		if labelsNode.Kind != yaml.MappingNode {
			errs = append(errs, typeErr(file, labelsNode.Line, "metadata.labels", "object"))
		} else {
			for i := 0; i+1 < len(labelsNode.Content); i += 2 {
				k := labelsNode.Content[i]
				v := labelsNode.Content[i+1]
				if k.Kind != yaml.ScalarNode {
					errs = append(errs, typeErr(file, k.Line, "metadata.labels", "object"))
					break
				}
				if v.Kind != yaml.ScalarNode {
					errs = append(errs, typeErr(file, v.Line, "metadata.labels."+k.Value, "string"))
				}
			}
		}
	}

	return errs
}

func validatePodSpec(file string, spec *yaml.Node) []ValidationError {
	var errs []ValidationError

	if osNode, ok := mapGet(spec, "os"); ok {
		if osNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, osNode.Line, "spec.os", "string"))
		} else {
			v := strings.TrimSpace(osNode.Value)
			if v != "linux" && v != "windows" {
				errs = append(errs, unsupported(file, osNode.Line, "spec.os", osNode.Value))
			}
		}
	}

	containersNode, ok := mapGet(spec, "containers")
	if !ok {
		errs = append(errs, req(file, "spec.containers"))
		return errs
	}
	if containersNode.Kind != yaml.SequenceNode {
		errs = append(errs, typeErr(file, containersNode.Line, "spec.containers", "array"))
		return errs
	}

	seenNames := map[string]struct{}{}
	for _, item := range containersNode.Content {
		if item.Kind != yaml.MappingNode {
			errs = append(errs, typeErr(file, item.Line, "containers", "object"))
			continue
		}
		errs = append(errs, validateContainer(file, item, seenNames)...)
	}
	return errs
}

func validatePodOS(file string, osNode *yaml.Node) []ValidationError {
	var errs []ValidationError
	nameNode, ok := mapGet(osNode, "name")
	if !ok {
		errs = append(errs, req(file, "spec.os.name"))
		return errs
	}
	if nameNode.Kind != yaml.ScalarNode {
		errs = append(errs, typeErr(file, nameNode.Line, "spec.os.name", "string"))
		return errs
	}
	v := strings.TrimSpace(nameNode.Value)
	if v != "linux" && v != "windows" {
		errs = append(errs, unsupported(file, nameNode.Line, "spec.os.name", nameNode.Value))
	}
	return errs
}

var (
	snakeCaseRe = regexp.MustCompile(`^[a-z]+(_[a-z0-9]+)*$`)
	memRe       = regexp.MustCompile(`^[0-9]+(Gi|Mi|Ki)$`)
)

func validateContainer(file string, c *yaml.Node, seen map[string]struct{}) []ValidationError {
	var errs []ValidationError

	nameNode, ok := mapGet(c, "name")
	if !ok {
		errs = append(errs, ValidationError{File: file, Line: 0, Msg: "name is required"})
	} else if nameNode.Kind != yaml.ScalarNode {
		errs = append(errs, typeErr(file, nameNode.Line, "name", "string"))
	} else {
		n := strings.TrimSpace(nameNode.Value)
		if n == "" {
			errs = append(errs, ValidationError{
				File: file,
				Line: nameNode.Line,
				Msg:  "name is required",
			})
		} else if !snakeCaseRe.MatchString(n) {
			errs = append(errs, invalidFormat(file, nameNode.Line, "name", nameNode.Value))
		}
	}

	imageNode, ok := mapGet(c, "image")
	if !ok {
		errs = append(errs, req(file, "containers.image"))
	} else {
		if imageNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, imageNode.Line, "containers.image", "string"))
		} else {
			img := strings.TrimSpace(imageNode.Value)
			if !validImage(img) {
				errs = append(errs, invalidFormat(file, imageNode.Line, "containers.image", imageNode.Value))
			}
		}
	}

	if portsNode, ok := mapGet(c, "ports"); ok {
		if portsNode.Kind != yaml.SequenceNode {
			errs = append(errs, typeErr(file, portsNode.Line, "ports", "array"))
		} else {
			for _, p := range portsNode.Content {
				if p.Kind != yaml.MappingNode {
					errs = append(errs, typeErr(file, p.Line, "ports", "object"))
					continue
				}
				errs = append(errs, validateContainerPort(file, p)...)
			}
		}
	}

	if rp, ok := mapGet(c, "readinessProbe"); ok {
		if rp.Kind != yaml.MappingNode {
			errs = append(errs, typeErr(file, rp.Line, "readinessProbe", "object"))
		} else {
			errs = append(errs, validateProbe(file, rp, "readinessProbe")...)
		}
	}

	if lp, ok := mapGet(c, "livenessProbe"); ok {
		if lp.Kind != yaml.MappingNode {
			errs = append(errs, typeErr(file, lp.Line, "livenessProbe", "object"))
		} else {
			errs = append(errs, validateProbe(file, lp, "livenessProbe")...)
		}
	}

	resNode, ok := mapGet(c, "resources")
	if !ok {
		errs = append(errs, req(file, "resources"))
	} else {
		if resNode.Kind != yaml.MappingNode {
			errs = append(errs, typeErr(file, resNode.Line, "resources", "object"))
		} else {
			errs = append(errs, validateResources(file, resNode)...)
		}
	}

	return errs
}

func validateContainerPort(file string, p *yaml.Node) []ValidationError {
	var errs []ValidationError

	cpNode, ok := mapGet(p, "containerPort")
	if !ok {
		errs = append(errs, req(file, "containerPort"))
		return errs
	}
	if cpNode.Kind != yaml.ScalarNode {
		errs = append(errs, typeErr(file, cpNode.Line, "containerPort", "int"))
		return errs
	}
	cp, err := strconv.Atoi(strings.TrimSpace(cpNode.Value))
	if err != nil {
		errs = append(errs, typeErr(file, cpNode.Line, "containerPort", "int"))
		return errs
	}
	if cp <= 0 || cp >= 65536 {
		errs = append(errs, outOfRange(file, cpNode.Line, "containerPort"))
	}

	if protoNode, ok := mapGet(p, "protocol"); ok {
		if protoNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, protoNode.Line, "protocol", "string"))
		} else {
			v := strings.TrimSpace(protoNode.Value)
			if v != "TCP" && v != "UDP" {
				errs = append(errs, unsupported(file, protoNode.Line, "protocol", protoNode.Value))
			}
		}
	}

	return errs
}

func validateProbe(file string, probe *yaml.Node, field string) []ValidationError {
	var errs []ValidationError
	httpGetNode, ok := mapGet(probe, "httpGet")
	if !ok {
		errs = append(errs, req(file, field+".httpGet"))
		return errs
	}
	if httpGetNode.Kind != yaml.MappingNode {
		errs = append(errs, typeErr(file, httpGetNode.Line, field+".httpGet", "object"))
		return errs
	}
	errs = append(errs, validateHTTPGetAction(file, httpGetNode)...)
	return errs
}

func validateHTTPGetAction(file string, h *yaml.Node) []ValidationError {
	var errs []ValidationError

	pathNode, ok := mapGet(h, "path")
	if !ok {
		errs = append(errs, req(file, "httpGet.path"))
	} else {
		if pathNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, pathNode.Line, "httpGet.path", "string"))
		} else {
			v := strings.TrimSpace(pathNode.Value)
			if !strings.HasPrefix(v, "/") {
				errs = append(errs, invalidFormat(file, pathNode.Line, "httpGet.path", pathNode.Value))
			}
		}
	}

	portNode, ok := mapGet(h, "port")
	if !ok {
		errs = append(errs, req(file, "httpGet.port"))
	} else {
		if portNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, portNode.Line, "httpGet.port", "int"))
		} else {
			p, err := strconv.Atoi(strings.TrimSpace(portNode.Value))
			if err != nil {
				errs = append(errs, typeErr(file, portNode.Line, "httpGet.port", "int"))
			} else if p <= 0 || p >= 65536 {
				errs = append(errs, outOfRange(file, portNode.Line, "httpGet.port"))
			}
		}
	}

	return errs
}

func validateResources(file string, r *yaml.Node) []ValidationError {
	var errs []ValidationError

	if reqNode, ok := mapGet(r, "requests"); ok {
		if reqNode.Kind != yaml.MappingNode {
			errs = append(errs, typeErr(file, reqNode.Line, "requests", "object"))
		} else {
			errs = append(errs, validateResourceMap(file, reqNode, "requests")...)
		}
	}

	if limNode, ok := mapGet(r, "limits"); ok {
		if limNode.Kind != yaml.MappingNode {
			errs = append(errs, typeErr(file, limNode.Line, "limits", "object"))
		} else {
			errs = append(errs, validateResourceMap(file, limNode, "limits")...)
		}
	}

	return errs
}

func validateResourceMap(file string, n *yaml.Node, prefix string) []ValidationError {
	var errs []ValidationError

	if cpuNode, ok := mapGet(n, "cpu"); ok {
		if cpuNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, cpuNode.Line, "cpu", "int"))
		} else {
			if _, err := strconv.Atoi(strings.TrimSpace(cpuNode.Value)); err != nil {
				errs = append(errs, typeErr(file, cpuNode.Line, "cpu", "int"))
			}
		}
	}

	if memNode, ok := mapGet(n, "memory"); ok {
		if memNode.Kind != yaml.ScalarNode {
			errs = append(errs, typeErr(file, memNode.Line, "memory", "string"))
		} else {
			v := strings.TrimSpace(memNode.Value)
			if !memRe.MatchString(v) {
				errs = append(errs, invalidFormat(file, memNode.Line, "memory", memNode.Value))
			}
		}
	}

	return errs
}

func validImage(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "registry.bigbrother.io/") {
		return false
	}
	slash := strings.LastIndex(s, "/")
	colon := strings.LastIndex(s, ":")
	if colon <= slash {
		return false
	}
	tag := s[colon+1:]
	return tag != ""
}

func mapGet(m *yaml.Node, key string) (*yaml.Node, bool) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		v := m.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return v, true
		}
	}
	return nil, false
}

func req(file, field string) ValidationError {
	return ValidationError{File: file, Line: 0, Msg: field + " is required"}
}

func typeErr(file string, line int, field, typ string) ValidationError {
	return ValidationError{File: file, Line: line, Msg: field + " must be " + typ}
}

func invalidFormat(file string, line int, field, orig string) ValidationError {
	return ValidationError{File: file, Line: line, Msg: field + " has invalid format '" + orig + "'"}
}

func unsupported(file string, line int, field, val string) ValidationError {
	return ValidationError{File: file, Line: line, Msg: field + " has unsupported value '" + val + "'"}
}

func outOfRange(file string, line int, field string) ValidationError {
	return ValidationError{File: file, Line: line, Msg: field + " value out of range"}
}
