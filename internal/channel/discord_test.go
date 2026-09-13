package channel

import "testing"

func TestValidateDiscordHost(t *testing.T) {
	valid := []string{
		"https://discord.com/api/webhooks/123/abc",
		"https://discordapp.com/api/webhooks/123/abc",
		"https://ptb.discord.com/api/webhooks/123/abc",
	}
	for _, target := range valid {
		if err := validateDiscordHost(target); err != nil {
			t.Errorf("esperava %q válido, obteve erro: %v", target, err)
		}
	}

	invalid := []string{
		"https://evil.com/api/webhooks/123/abc",
		"https://discord.com.evil.com/hook", // host completo não termina em discord.com
		"http://169.254.169.254/latest/meta-data",
		"não é uma url",
	}
	for _, target := range invalid {
		if err := validateDiscordHost(target); err == nil {
			t.Errorf("esperava %q inválido (fora do domínio discord), mas passou", target)
		}
	}
}
