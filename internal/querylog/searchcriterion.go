package querylog

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/AdguardTeam/AdGuardHome/internal/filtering"
)

type criterionType int

const (
	// ctDomainOrClient is for searching by the domain name, the client's IP
	// address, or the clinet's ID.
	ctDomainOrClient criterionType = iota
	// ctFilteringStatus is for searching by the filtering status.
	//
	// See (*searchCriterion).ctFilteringStatusCase for details.
	ctFilteringStatus
)

const (
	filteringStatusAll      = "all"
	filteringStatusFiltered = "filtered" // all kinds of filtering

	filteringStatusBlocked             = "blocked"              // blocked or blocked services
	filteringStatusBlockedService      = "blocked_services"     // blocked
	filteringStatusBlockedSafebrowsing = "blocked_safebrowsing" // blocked by safebrowsing
	filteringStatusBlockedParental     = "blocked_parental"     // blocked by parental control
	filteringStatusWhitelisted         = "whitelisted"          // whitelisted
	filteringStatusRewritten           = "rewritten"            // all kinds of rewrites
	filteringStatusSafeSearch          = "safe_search"          // enforced safe search
	filteringStatusProcessed           = "processed"            // not blocked, not white-listed entries
)

// filteringStatusValues -- array with all possible filteringStatus values
var filteringStatusValues = []string{
	filteringStatusAll, filteringStatusFiltered, filteringStatusBlocked,
	filteringStatusBlockedService, filteringStatusBlockedSafebrowsing, filteringStatusBlockedParental,
	filteringStatusWhitelisted, filteringStatusRewritten, filteringStatusSafeSearch,
	filteringStatusProcessed,
}

// searchCriterion is a search criterion that is used to match a record.
type searchCriterion struct {
	value         string
	criterionType criterionType
	// strict, if true, means that the criterion must be applied to the
	// whole value rather than the part of it.  That is, equality and not
	// containment.
	strict bool
}

func (c *searchCriterion) ctDomainOrClientCaseStrict(
	term string,
	clientID string,
	name string,
	host string,
	ip string,
) (ok bool) {
	return strings.EqualFold(host, term) ||
		strings.EqualFold(clientID, term) ||
		strings.EqualFold(ip, term) ||
		strings.EqualFold(name, term)
}

// containsFold reports whehter s contains, ignoring letter case, substr.
//
// TODO(a.garipov): Move to aghstrings if needed elsewhere.
func containsFold(s, substr string) (ok bool) {
	sLen, substrLen := len(s), len(substr)
	if sLen < substrLen {
		return false
	}

	if sLen == substrLen {
		return strings.EqualFold(s, substr)
	}

	first, _ := utf8.DecodeRuneInString(substr)
	firstFolded := unicode.SimpleFold(first)

	for i := 0; i != -1 && len(s) >= len(substr); {
		if strings.EqualFold(s[:substrLen], substr) {
			return true
		}

		i = strings.IndexFunc(s[1:], func(r rune) (eq bool) {
			return r == first || r == firstFolded
		})

		s = s[1+i:]
	}

	return false
}

func (c *searchCriterion) ctDomainOrClientCaseNonStrict(
	term string,
	clientID string,
	name string,
	host string,
	ip string,
) (ok bool) {
	return containsFold(clientID, term) ||
		containsFold(host, term) ||
		containsFold(ip, term) ||
		containsFold(name, term)
}

// quickMatch quickly checks if the line matches the given search criterion.
// It returns false if the like doesn't match.  This method is only here for
// optimisation purposes.
func (c *searchCriterion) quickMatch(line string, findClient quickMatchClientFunc) (ok bool) {
	switch c.criterionType {
	case ctDomainOrClient:
		host := readJSONValue(line, `"QH":"`)
		ip := readJSONValue(line, `"IP":"`)
		clientID := readJSONValue(line, `"CID":"`)

		var name string
		if cli := findClient(clientID, ip); cli != nil {
			name = cli.Name
		}

		if c.strict {
			return c.ctDomainOrClientCaseStrict(c.value, clientID, name, host, ip)
		}

		return c.ctDomainOrClientCaseNonStrict(c.value, clientID, name, host, ip)
	case ctFilteringStatus:
		// Go on, as we currently don't do quick matches against
		// filtering statuses.
		return true
	default:
		return true
	}
}

// match checks if the log entry matches this search criterion.
func (c *searchCriterion) match(entry *logEntry) bool {
	switch c.criterionType {
	case ctDomainOrClient:
		return c.ctDomainOrClientCase(entry)
	case ctFilteringStatus:
		return c.ctFilteringStatusCase(entry.Result)
	}

	return false
}

func (c *searchCriterion) ctDomainOrClientCase(e *logEntry) bool {
	clientID := e.ClientID
	host := e.QHost

	var name string
	if e.client != nil {
		name = e.client.Name
	}

	ip := e.IP.String()
	term := strings.ToLower(c.value)
	if c.strict {
		return c.ctDomainOrClientCaseStrict(term, clientID, name, host, ip)
	}

	return c.ctDomainOrClientCaseNonStrict(term, clientID, name, host, ip)
}

func (c *searchCriterion) ctFilteringStatusCase(res filtering.Result) bool {
	switch c.value {
	case filteringStatusAll:
		return true

	case filteringStatusFiltered:
		return res.IsFiltered ||
			res.Reason.In(
				filtering.NotFilteredAllowList,
				filtering.Rewritten,
				filtering.RewrittenAutoHosts,
				filtering.RewrittenRule,
			)

	case filteringStatusBlocked:
		return res.IsFiltered &&
			res.Reason.In(filtering.FilteredBlockList, filtering.FilteredBlockedService)

	case filteringStatusBlockedService:
		return res.IsFiltered && res.Reason == filtering.FilteredBlockedService

	case filteringStatusBlockedParental:
		return res.IsFiltered && res.Reason == filtering.FilteredParental

	case filteringStatusBlockedSafebrowsing:
		return res.IsFiltered && res.Reason == filtering.FilteredSafeBrowsing

	case filteringStatusWhitelisted:
		return res.Reason == filtering.NotFilteredAllowList

	case filteringStatusRewritten:
		return res.Reason.In(
			filtering.Rewritten,
			filtering.RewrittenAutoHosts,
			filtering.RewrittenRule,
		)

	case filteringStatusSafeSearch:
		return res.IsFiltered && res.Reason == filtering.FilteredSafeSearch

	case filteringStatusProcessed:
		return !res.Reason.In(
			filtering.FilteredBlockList,
			filtering.FilteredBlockedService,
			filtering.NotFilteredAllowList,
		)

	default:
		return false
	}
}