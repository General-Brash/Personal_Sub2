package service

import (
	"strings"
)

type contentModerationKeywordMatcher struct {
	nodes           []contentModerationKeywordNode
	edges           []contentModerationKeywordEdge
	rootTransitions [256]int32
	keywords        []string
	keywordRules    []contentModerationKeywordRule
}

type contentModerationKeywordNode struct {
	failure         int32
	terminalKeyword int32
	bestKeyword     int32
	edgeStart       uint32
	edgeCount       uint16
}

type contentModerationKeywordRule struct {
	lower                []byte
	requireLeftBoundary  bool
	requireRightBoundary bool
}

type contentModerationKeywordEdge struct {
	target int32
	label  byte
}

type contentModerationKeywordBuildEdge struct {
	target      int32
	nextSibling int32
	label       byte
}

func newContentModerationKeywordMatcher(keywords []string) *contentModerationKeywordMatcher {
	if len(keywords) == 0 {
		return nil
	}

	buildNodes := []contentModerationKeywordNode{newContentModerationKeywordNode()}
	buildEdges := make([]contentModerationKeywordBuildEdge, 0)
	originalKeywords := append([]string(nil), keywords...)
	keywordRules := make([]contentModerationKeywordRule, len(keywords))

	for keywordIndex, keyword := range keywords {
		if keyword == "" {
			continue
		}
		rule := newContentModerationKeywordRule(keyword)
		keywordRules[keywordIndex] = rule
		state := int32(0)
		for _, label := range rule.lower {
			next := contentModerationKeywordBuildTransition(buildNodes, buildEdges, state, label)
			if next < 0 {
				next = int32(len(buildNodes))
				buildNodes = append(buildNodes, newContentModerationKeywordNode())
				buildEdges = append(buildEdges, contentModerationKeywordBuildEdge{
					target:      next,
					nextSibling: contentModerationKeywordBuildFirstEdge(buildNodes[state]),
					label:       label,
				})
				buildNodes[state].edgeStart = uint32(len(buildEdges))
			}
			state = next
		}
		if current := buildNodes[state].terminalKeyword; current < 0 || int32(keywordIndex) < current {
			buildNodes[state].terminalKeyword = int32(keywordIndex)
			buildNodes[state].bestKeyword = int32(keywordIndex)
		}
	}

	if len(buildNodes) == 1 {
		return nil
	}

	queue := make([]int32, 0, len(buildNodes)-1)
	var rootTransitions [256]int32
	for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[0]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
		edge := buildEdges[edgeIndex]
		rootTransitions[edge.label] = edge.target
		queue = append(queue, edge.target)
	}

	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		state := queue[queueIndex]
		for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[state]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			failure := buildNodes[state].failure
			fallback := contentModerationKeywordBuildTransition(buildNodes, buildEdges, failure, edge.label)
			for fallback < 0 && failure != 0 {
				failure = buildNodes[failure].failure
				fallback = contentModerationKeywordBuildTransition(buildNodes, buildEdges, failure, edge.label)
			}
			if fallback >= 0 {
				buildNodes[edge.target].failure = fallback
			}
			buildNodes[edge.target].bestKeyword = minKeywordIndex(
				buildNodes[edge.target].bestKeyword,
				buildNodes[buildNodes[edge.target].failure].bestKeyword,
			)
			queue = append(queue, edge.target)
		}
	}

	edges := make([]contentModerationKeywordEdge, 0, len(buildEdges))
	var outgoing [256]contentModerationKeywordEdge
	for nodeIndex := range buildNodes {
		count := 0
		for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[nodeIndex]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			outgoing[count] = contentModerationKeywordEdge{target: edge.target, label: edge.label}
			count++
		}
		for index := 1; index < count; index++ {
			current := outgoing[index]
			insertAt := index
			for insertAt > 0 && current.label < outgoing[insertAt-1].label {
				outgoing[insertAt] = outgoing[insertAt-1]
				insertAt--
			}
			outgoing[insertAt] = current
		}
		buildNodes[nodeIndex].edgeStart = uint32(len(edges))
		buildNodes[nodeIndex].edgeCount = uint16(count)
		edges = append(edges, outgoing[:count]...)
	}

	return &contentModerationKeywordMatcher{
		nodes:           buildNodes,
		edges:           edges,
		rootTransitions: rootTransitions,
		keywords:        originalKeywords,
		keywordRules:    keywordRules,
	}
}

func newContentModerationKeywordNode() contentModerationKeywordNode {
	return contentModerationKeywordNode{terminalKeyword: -1, bestKeyword: -1}
}

func contentModerationKeywordBuildFirstEdge(node contentModerationKeywordNode) int32 {
	if node.edgeStart == 0 {
		return -1
	}
	return int32(node.edgeStart - 1)
}

func contentModerationKeywordBuildTransition(
	nodes []contentModerationKeywordNode,
	edges []contentModerationKeywordBuildEdge,
	state int32,
	label byte,
) int32 {
	if state < 0 || int(state) >= len(nodes) {
		return -1
	}
	for edgeIndex := contentModerationKeywordBuildFirstEdge(nodes[state]); edgeIndex >= 0; edgeIndex = edges[edgeIndex].nextSibling {
		if edges[edgeIndex].label == label {
			return edges[edgeIndex].target
		}
	}
	return -1
}

func minKeywordIndex(left, right int32) int32 {
	if left < 0 {
		return right
	}
	if right < 0 || left < right {
		return left
	}
	return right
}

func (m *contentModerationKeywordMatcher) Match(text string) (string, bool) {
	if m == nil || text == "" || len(m.nodes) == 0 || len(m.keywords) == 0 {
		return "", false
	}
	lowerBytes := []byte(strings.ToLower(text))
	state := int32(0)
	bestKeyword := int32(-1)
	for index, label := range lowerBytes {
		for {
			next := m.next(state, label)
			if next != 0 {
				state = next
				break
			}
			if state == 0 {
				break
			}
			state = m.nodes[state].failure
		}
		candidate := m.nodes[state].bestKeyword
		if candidate >= 0 && (bestKeyword < 0 || candidate < bestKeyword) {
			if m.keywordMatchesAt(candidate, lowerBytes, index) {
				bestKeyword = candidate
			} else {
				bestKeyword = m.bestBoundaryMatch(state, lowerBytes, index, bestKeyword)
			}
		}
		if bestKeyword == 0 {
			return m.keywords[0], true
		}
	}
	if bestKeyword < 0 || int(bestKeyword) >= len(m.keywords) {
		return "", false
	}
	return m.keywords[bestKeyword], true
}

func (m *contentModerationKeywordMatcher) bestBoundaryMatch(
	state int32,
	text []byte,
	end int,
	bestKeyword int32,
) int32 {
	for state != 0 {
		candidate := m.nodes[state].terminalKeyword
		if candidate >= 0 && (bestKeyword < 0 || candidate < bestKeyword) && m.keywordMatchesAt(candidate, text, end) {
			bestKeyword = candidate
		}
		state = m.nodes[state].failure
	}
	return bestKeyword
}

func (m *contentModerationKeywordMatcher) keywordMatchesAt(keywordIndex int32, text []byte, end int) bool {
	if keywordIndex < 0 || int(keywordIndex) >= len(m.keywordRules) {
		return false
	}
	rule := m.keywordRules[keywordIndex]
	start := end + 1 - len(rule.lower)
	return contentModerationKeywordRuleMatchesAt(text, start, rule)
}

func newContentModerationKeywordRule(keyword string) contentModerationKeywordRule {
	lower := []byte(strings.ToLower(keyword))
	rule := contentModerationKeywordRule{lower: lower}
	if len(lower) == 0 {
		return rule
	}
	// ASCII terms use word boundaries; non-ASCII endpoints retain substring matching.
	rule.requireLeftBoundary = isASCIIWordByte(lower[0])
	rule.requireRightBoundary = isASCIIWordByte(lower[len(lower)-1])
	return rule
}

func contentModerationKeywordRuleMatchesAt(text []byte, start int, rule contentModerationKeywordRule) bool {
	if len(rule.lower) == 0 || start < 0 || start+len(rule.lower) > len(text) {
		return false
	}
	end := start + len(rule.lower)
	if rule.requireLeftBoundary && start > 0 && isASCIIWordByte(text[start-1]) {
		return false
	}
	if rule.requireRightBoundary && end < len(text) && isASCIIWordByte(text[end]) {
		return false
	}
	return true
}

func isASCIIWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_'
}

func (m *contentModerationKeywordMatcher) next(state int32, label byte) int32 {
	if state == 0 {
		return m.rootTransitions[label]
	}
	if state < 0 || int(state) >= len(m.nodes) {
		return 0
	}
	node := m.nodes[state]
	left := int(node.edgeStart)
	right := left + int(node.edgeCount)
	for left < right {
		middle := left + (right-left)/2
		edge := m.edges[middle]
		if edge.label < label {
			left = middle + 1
			continue
		}
		right = middle
	}
	end := int(node.edgeStart) + int(node.edgeCount)
	if left < end && m.edges[left].label == label {
		return m.edges[left].target
	}
	return 0
}
