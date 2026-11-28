package site

import (
	"strings"
	"unicode"
)

// helpLines are the Malaysian help lines named in the crisis reply and in
// every bot's system prompt (chat_prompts.go).
var helpLines = []string{
	"999 (kecemasan)",
	"Befrienders KL: 03-7627 2929 (24 jam)",
	"Talian HEAL 15555 (KKM, 8 pagi hingga 12 malam)",
	"Talian Kasih 15999 atau WhatsApp 019-261 5999",
}

// crisisPhrases are matched as whole words against the lowercased message.
// Multi-word on purpose: bare "mati" or "bunuh" would fire on "mati kutu" or
// "bos nak bunuh saya dengan kerja".
var crisisPhrases = []string{
	"bunuh diri", "nak mati", "nk mati", "ingin mati", "mahu mati", "nak bunuh diri",
	"tak nak hidup", "tak nk hidup", "x nak hidup", "tak mahu hidup", "tidak mahu hidup",
	"akhiri hidup", "tamatkan hidup", "cederakan diri", "sakiti diri", "kelar tangan",
	// Suffixed forms, common typos and euphemisms. "tak larat nak teruskan" alone
	// is everyday work stress, so it needs "hidup" after it.
	"bunuh dirinya", "membunuh diri", "bunuh dri", "bnuh diri", "tk nak hidup", "tak nak hdup", "tak mau hidup", "x mahu hidup",
	"tak larat nak hidup", "tak larat nak teruskan hidup", "tidur selamanya", "tidur selama-lamanya",
	"kill myself", "end my life", "suicide", "suicidal", "self-harm", "self harm", "hurt myself", "want to die",
}

// ponytail: a plain keyword list catches the obvious statements only (no
// typos, no euphemisms); the system prompt's crisis rule is the second layer.
// Grow the list from real misses.
func isCrisis(msg string) bool {
	// Punctuation becomes a space and phrases match whole words, so "nak mati."
	// hits but "nak matikan lampu" does not.
	m := " " + strings.Join(strings.FieldsFunc(strings.ToLower(msg), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-'
	}), " ") + " "
	for _, p := range crisisPhrases {
		if strings.Contains(m, " "+p+" ") {
			return true
		}
	}
	return false
}

// crisisReply is the fixed answer to a message isCrisis flags. The model is
// not called.
var crisisReply = "Terima kasih kerana berkongsi. Apa yang anda rasa ini sangat penting, dan saya prihatin tentang keselamatan anda.\n\n" +
	"Saya ialah bot AI, bukan perkhidmatan krisis, dan saya tidak dapat memberi sokongan yang anda perlukan sekarang. Sila hubungi seseorang yang anda percayai, seperti ahli keluarga atau rakan, dan jangan tanggung seorang diri. Anda juga boleh menghubungi talian bantuan ini, percuma dan sedia mendengar:\n\n" +
	"- " + strings.Join(helpLines, "\n- ") + "\n\n" +
	"Jika anda dalam bahaya sekarang, hubungi 999 atau pergi ke hospital terdekat.\n\n" +
	"Apabila anda sudah bersedia, anda boleh kembali berbual dengan saya di sini."
