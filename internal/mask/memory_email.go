package mask

import "strings"

// What a masked e-mail teaches (see docs/structures.md, section What an e-mail teaches). The address
// says two things about the text around it:
//
//   - its domain is the organization's: "ana.souza@acme.io" masked and "tenant = acme.io" two lines
//     below in plain text tell the model the same thing. The domain is learned and masked wherever it
//     appears, in any case (a DNS name is case-insensitive, RFC 4343), with the pseudonym the
//     address already has after the "@" (d<id>.invalid), so the model still sees they are the same.
//     The domains of personal mail accounts (gmail.com, outlook.com...) say nothing about the
//     organization and stay, like any public domain;
//   - a local part "first.last" is the person's name: "Ana Souza" in a git log, a commit or an
//     approval list is the same person. It enters the people registry (as import-people does) tied
//     to the address, and the name and the e-mail get the same identifier. A mailbox that is a role
//     (support.team, no.reply) or an area (bi.vendas, ti.suporte) is not a person.

// personalMail: the domains of personal mail accounts, as each provider documents them. Google:
// gmail.com and googlemail.com are the same account (support.google.com/mail/answer/10313).
// Microsoft: Outlook.com accounts end in @outlook.com, @hotmail.com, @live.com or @msn.com
// (support.microsoft.com, "Do you support Hotmail, MSN, and Live?"). Apple: iCloud Mail addresses are
// @icloud.com, @me.com or @mac.com (support.apple.com/118230). Other providers whose organization is
// a vendor (yahoo, protonmail) are caught by publicMailDomain.
var personalMail = setOf("gmail.com", "googlemail.com", "outlook.com", "hotmail.com", "live.com", "msn.com",
	"icloud.com", "me.com", "mac.com")

// roleMailboxes: mailbox names of a function, not of a person: the ones RFC 2142 defines (info,
// marketing, sales, support, abuse, noc, security, postmaster, hostmaster, usenet, news, webmaster,
// www, uucp, ftp) and the usual automatic senders.
var roleMailboxes = setOf("info", "marketing", "sales", "support", "abuse", "noc", "security", "postmaster",
	"hostmaster", "usenet", "news", "webmaster", "www", "uucp", "ftp", "no", "noreply", "reply", "donotreply",
	"mailer", "daemon", "bounce", "bounces", "notifications", "notification", "alerts", "alert")

// learnEmail: what the masked address e teaches (domain and name).
func (m *Masker) learnEmail(e string) {
	at := strings.LastIndexByte(e, '@')
	if at <= 0 || at == len(e)-1 || m.allowedEmail(e) {
		return
	}
	m.learnMailDomain(e[at+1:])
	m.learnMailName(e[:at], e)
}

// learnMailDomain: the organization's domain, in the spelling it came in and in both cases. Only a
// name under a real TLD (the IANA list of publicDomain, or a country), and not a file name or a
// reserved one: "\n@pytest.fixture" and "x@main.py" in escaped code look like an e-mail and are not
// a domain (.py is also Paraguay's: a file extension wins).
func (m *Masker) learnMailDomain(d string) {
	l := strings.ToLower(strings.TrimRight(d, "."))
	tld := l[strings.LastIndexByte(l, '.')+1:]
	if strings.IndexByte(l, '.') < 0 || !publicDomain(l) || reservedTLDs[tld] || fileExtensions[tld] ||
		publicMailDomain(l) || !simpleName(l) {
		return
	}
	for _, v := range []string{d, l, strings.ToUpper(l)} {
		if len(v) >= 4 {
			m.known.learnIt("domain", v)
		}
	}
}

// publicMailDomain: d (lowercase) is a personal mail domain, or the domain of a vendor or of a
// public reference (the organization is public: github.com, yahoo.com, microsoft.com).
func publicMailDomain(d string) bool {
	if personalMail[d] {
		return true
	}
	o := registrableLabel(d)
	return publicRef[o] || vendors[normSoftware(o)]
}

// registrableLabel: the organization's label of a domain: the one before the suffix ("acme" in
// acme.io, acme.com.br and mail.acme.co.uk).
func registrableLabel(d string) string {
	ls := strings.Split(d, ".")
	i := len(ls) - 2
	if i > 0 && len(ls[len(ls)-1]) == 2 && secondLevel[ls[i]] {
		i-- // country with second level: .com.br, .co.uk
	}
	if i < 0 {
		return d
	}
	return ls[i]
}

var secondLevel = setOf("com", "net", "org", "gov", "edu", "co", "ac", "mil", "nom", "art", "ind", "inf")

// learnMailName: the person of a local part "first.last" (or first_last), tied to the address. Only
// when neither the name nor the address is in the registry yet: one already there keeps its
// identifier (the pseudonym of the e-mail does not change in the middle of the conversation).
func (m *Masker) learnMailName(local, email string) {
	if m.people == nil {
		return
	}
	ps := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' })
	if len(ps) != 2 || len(ps[0])+len(ps[1]) != len(local)-1 {
		return
	}
	for _, p := range ps {
		// a 2-letter piece is the acronym of an area (bi.vendas, ti.suporte, rh.vagas), not a name
		if len(p) < 3 || len(p) > 30 || roleMailboxes[strings.ToLower(p)] || publicDev(p) {
			return
		}
		for i := 0; i < len(p); i++ {
			if !letterD(p[i]) {
				return
			}
		}
	}
	name := capitalize(ps[0]) + " " + capitalize(ps[1])
	if m.people.PID(m.p, "name", NormName(name)) != "" || m.people.PID(m.p, "email", strings.ToLower(email)) != "" {
		return
	}
	m.people.Import(m.p, name, email, "")
	// the texts already memoized with the name are redone (see learnIt)
	m.known.learnIt("name", name)
	m.known.learnIt("name", strings.ToUpper(name))
}
