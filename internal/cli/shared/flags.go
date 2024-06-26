package shared

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// Note: Don't use pointers for flags, because they have own state which is not thread-safe.
// https://github.com/urfave/cli/issues/1926

var ListenAddrFlag = cli.StringFlag{
	Name:     "listen",
	Aliases:  []string{"l"},
	Usage:    "IP (v4 or v6) address to listen on",
	Value:    "0.0.0.0", // bind to all interfaces by default
	Sources:  cli.EnvVars("LISTEN_ADDR"),
	OnlyOnce: true,
	Config:   cli.StringConfig{TrimSpace: true},
	Validator: func(ip string) error {
		if ip == "" {
			return fmt.Errorf("missing IP address")
		}

		if net.ParseIP(ip) == nil {
			return fmt.Errorf("wrong IP address [%s] for listening", ip)
		}

		return nil
	},
}

var ListenPortFlag = cli.UintFlag{
	Name:     "port",
	Aliases:  []string{"p"},
	Usage:    "TCP port number",
	Value:    8080, // default port number
	Sources:  cli.EnvVars("LISTEN_PORT"),
	OnlyOnce: true,
	Validator: func(port uint64) error {
		if port == 0 || port > 65535 {
			return fmt.Errorf("wrong TCP port number [%d]", port)
		}

		return nil
	},
}

var AddTemplatesFlag = cli.StringSliceFlag{
	Name: "add-template",
	Usage: "to add a new template, provide the path to the file using this flag (the filename without the extension " +
		"will be used as the template name)",
	Config: cli.StringConfig{TrimSpace: true},
	Validator: func(paths []string) error {
		for _, path := range paths {
			if path == "" {
				return fmt.Errorf("missing template path")
			}

			if stat, err := os.Stat(path); err != nil || stat.IsDir() {
				return fmt.Errorf("wrong template path [%s]", path)
			}
		}

		return nil
	},
}

var DisableTemplateNamesFlag = cli.StringSliceFlag{
	Name:   "disable-template",
	Usage:  "disable the specified template by its name",
	Config: cli.StringConfig{TrimSpace: true},
}

var AddHTTPCodesFlag = cli.StringMapFlag{
	Name: "add-http-code",
	Usage: "to add a new HTTP status code, provide the code and its message/description using this flag (the format " +
		"should be '%code%=%message%/%description%'; the code may contain a wildcard '*' to cover multiple codes at once, " +
		"for example, '4**' will cover all 4xx codes, unless a more specific code was described previously)",
	Config: cli.StringConfig{TrimSpace: true},
	Validator: func(codes map[string]string) error {
		for code, msgAndDesc := range codes {
			if code == "" {
				return fmt.Errorf("missing HTTP code")
			} else if len(code) != 3 {
				return fmt.Errorf("wrong HTTP code [%s]: it should be 3 characters long", code)
			}

			if parts := strings.SplitN(msgAndDesc, "/", 3); len(parts) < 1 || len(parts) > 2 {
				return fmt.Errorf("wrong message/description format for HTTP code [%s]: %s", code, msgAndDesc)
			} else if parts[0] == "" {
				return fmt.Errorf("missing message for HTTP code [%s]", code)
			}
		}

		return nil
	},
}
