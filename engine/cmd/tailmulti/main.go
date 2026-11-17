// Command tailmulti controls the Tailmulti service from a terminal.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"tailmulti.local/engine/internal/controlclient"
)

// Set at build time from the app bundle's Info.plist.
var (
	version = "dev"
	build   = "dev"
)

const usage = `Usage: tailmulti [--socket PATH] <command>

Commands:
  status [--json]                   Show the switch, accounts and conflicts
  devices [ACCOUNT] [--online] [--json]
                                    List devices and their local addresses
  up                                Turn Tailmulti on
  down [--keep-dns]                 Turn Tailmulti off and restore normal DNS
  account list                      List accounts
  account add NAME                  Add an account
  account remove ACCOUNT            Remove an account
  login ACCOUNT [--print-url]       Sign an account in through the browser
  connect ACCOUNT                   Connect a signed-in account
  disconnect ACCOUNT                Disconnect one account
  doctor [--json]                   Show host setup checks and service state
  version                           Show this command's and the service's version

ACCOUNT is an account name, its ID, or a unique ID prefix.
--socket selects a preview service run by the current user.
`

type usageError string

func (e usageError) Error() string { return string(e) }

type env struct {
	stdout, stderr io.Writer
	open           func(string) error
	uid            int
	sleep          func(time.Duration)
	signInTimeout  time.Duration
}

func main() {
	e := env{stdout: os.Stdout, stderr: os.Stderr, uid: os.Getuid(), sleep: time.Sleep, signInTimeout: 30 * time.Second,
		open: func(u string) error { return exec.Command("/usr/bin/open", u).Run() }}
	os.Exit(run(context.Background(), os.Args[1:], e))
}

func run(ctx context.Context, args []string, e env) int {
	err := dispatch(ctx, args, e)
	var usageErr usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &usageErr):
		fmt.Fprintln(e.stderr, "tailmulti:", err)
		fmt.Fprint(e.stderr, "\n"+usage)
		return 2
	default:
		fmt.Fprintln(e.stderr, "tailmulti:", err)
		return 1
	}
}

// parse accepts boolean flags before or after positional arguments.
func parse(name string, args []string, flags map[string]*bool) ([]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for flagName, target := range flags {
		fs.BoolVar(target, flagName, false, "")
	}
	// Everything after "--" is positional, even if it looks like a flag.
	var positional, literal []string
	for i, arg := range args {
		if arg == "--" {
			args, literal = args[:i], args[i+1:]
			break
		}
	}
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, usageError(err.Error())
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return append(positional, literal...), nil
}

func dispatch(ctx context.Context, args []string, e env) error {
	global := flag.NewFlagSet("tailmulti", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	socket := global.String("socket", "", "")
	help := global.Bool("help", false, "")
	global.BoolVar(help, "h", false, "")
	if err := global.Parse(args); err != nil {
		return usageError(err.Error())
	}
	args = global.Args()
	if *help || len(args) == 0 || args[0] == "help" {
		fmt.Fprint(e.stdout, usage)
		return nil
	}
	c := controlclient.Client{Path: controlclient.ServicePath}
	if *socket != "" {
		c = controlclient.Client{Path: *socket, ServerUID: e.uid}
	}
	command, rest := args[0], args[1:]
	want := func(flags map[string]*bool, n int, what string) ([]string, error) {
		positional, err := parse(command, rest, flags)
		if err == nil && len(positional) != n {
			err = usageError("usage: tailmulti " + what)
		}
		return positional, err
	}
	switch command {
	case "status":
		var asJSON bool
		if _, err := want(map[string]*bool{"json": &asJSON}, 0, "status [--json]"); err != nil {
			return err
		}
		if asJSON {
			return printRaw(ctx, c, e, "/v1/status")
		}
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		printStatus(e.stdout, s)
		return nil
	case "devices":
		var asJSON, online bool
		positional, err := parse(command, rest, map[string]*bool{"json": &asJSON, "online": &online})
		if err != nil {
			return err
		}
		if len(positional) > 1 {
			return usageError("usage: tailmulti devices [ACCOUNT] [--online] [--json]")
		}
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		only := ""
		if len(positional) == 1 {
			a, err := resolve(s, positional[0])
			if err != nil {
				return err
			}
			only = a.ID
		}
		return printDevices(e.stdout, s, only, online, asJSON)
	case "up":
		if _, err := want(nil, 0, "up"); err != nil {
			return err
		}
		return up(ctx, c, e)
	case "down":
		var keepDNS bool
		if _, err := want(map[string]*bool{"keep-dns": &keepDNS}, 0, "down [--keep-dns]"); err != nil {
			return err
		}
		if err := c.StopNetwork(ctx, !keepDNS); err != nil {
			return err
		}
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		if s.Mode != "inactive" {
			return errors.New("the service hasn't confirmed the change; try again")
		}
		fmt.Fprintln(e.stdout, "Tailmulti is off.")
		return nil
	case "account", "accounts":
		return account(ctx, c, e, rest)
	case "login":
		var printURL bool
		positional, err := want(map[string]*bool{"print-url": &printURL}, 1, "login ACCOUNT [--print-url]")
		if err != nil {
			return err
		}
		return login(ctx, c, e, positional[0], printURL)
	case "connect", "disconnect":
		positional, err := want(nil, 1, command+" ACCOUNT")
		if err != nil {
			return err
		}
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		a, err := resolve(s, positional[0])
		if err != nil {
			return err
		}
		if err = c.Action(ctx, command, a.ID); err != nil {
			return err
		}
		if command == "connect" {
			fmt.Fprintf(e.stdout, "Connecting %s. Check progress with: tailmulti status\n", a.Name)
		} else {
			fmt.Fprintf(e.stdout, "Disconnected %s.\n", a.Name)
		}
		return nil
	case "doctor":
		var asJSON bool
		if _, err := want(map[string]*bool{"json": &asJSON}, 0, "doctor [--json]"); err != nil {
			return err
		}
		return doctor(ctx, c, e, asJSON)
	case "version":
		if _, err := want(nil, 0, "version"); err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "tailmulti %s (build %s)\n", version, build)
		if serviceBuild, err := c.ServiceBuild(ctx); err == nil {
			fmt.Fprintf(e.stdout, "service build %s\n", serviceBuild)
		} else if errors.Is(err, controlclient.ErrUnavailable) {
			fmt.Fprintln(e.stdout, "service not running")
		} else {
			fmt.Fprintln(e.stdout, "service build unknown:", err)
		}
		return nil
	}
	return usageError("unknown command " + command)
}

func account(ctx context.Context, c controlclient.Client, e env, args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	positional, err := parse("account", args[1:], nil)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(positional) != 0 {
			return usageError("usage: tailmulti account list")
		}
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		printAccounts(e.stdout, s)
		return nil
	case "add":
		if len(positional) != 1 {
			return usageError("usage: tailmulti account add NAME")
		}
		a, err := c.Add(ctx, positional[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "Added %s (%s). Sign in with: tailmulti login %s\n", a.Name, a.ID[:min(8, len(a.ID))], quote(a.Name))
		return nil
	case "remove", "rm":
		if len(positional) != 1 {
			return usageError("usage: tailmulti account remove ACCOUNT")
		}
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		a, err := resolve(s, positional[0])
		if err != nil {
			return err
		}
		if err = c.Action(ctx, "remove", a.ID); err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "Removed %s.\n", a.Name)
		return nil
	}
	return usageError("unknown account command " + args[0])
}

func up(ctx context.Context, c controlclient.Client, e env) error {
	s, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if s.Mode != "active" {
		report, err := c.Setup(ctx)
		if err != nil {
			return err
		}
		if !report.Ready {
			for _, check := range report.Checks {
				if check.State != "pass" {
					return errors.New(check.Title + ". " + check.Detail)
				}
			}
			return errors.New("this Mac isn't ready to connect; run: tailmulti doctor")
		}
		if err = c.StartNetwork(ctx); err != nil {
			return err
		}
		if s, err = c.Status(ctx); err != nil {
			return err
		}
	}
	if s.Mode != "active" {
		return errors.New("the service hasn't confirmed the change; try again")
	}
	if s.Health != "ready" {
		return errors.New(s.Health)
	}
	fmt.Fprintln(e.stdout, "Tailmulti is on.")
	return nil
}

// login starts the account's engine if needed, then waits for Tailscale's sign-in page.
func login(ctx context.Context, c controlclient.Client, e env, ref string, printURL bool) error {
	current := func(id string) (controlclient.Account, error) {
		s, err := c.Status(ctx)
		if err != nil {
			return controlclient.Account{}, err
		}
		if s.Mode != "active" || s.Health != "ready" {
			return controlclient.Account{}, errors.New("turn Tailmulti on before signing in: tailmulti up")
		}
		if id == "" {
			return resolve(s, ref)
		}
		for _, a := range s.Accounts {
			if a.ID == id {
				return a, nil
			}
		}
		return controlclient.Account{}, errors.New("this account is no longer available")
	}
	a, err := current("")
	if err != nil {
		return err
	}
	settled := func(a controlclient.Account) bool {
		switch phase(a) {
		case "Connected":
			fmt.Fprintf(e.stdout, "%s is already signed in and connected.\n", a.Name)
			return true
		case "Waiting for admin approval":
			fmt.Fprintf(e.stdout, "%s is signed in and waiting for an admin to approve this device.\n", a.Name)
			return true
		}
		return false
	}
	if settled(a) {
		return nil
	}
	if !a.Enabled {
		if err = c.Action(ctx, "connect", a.ID); err != nil {
			return err
		}
	}
	for waited := time.Duration(0); ; waited += 250 * time.Millisecond {
		if a, err = current(a.ID); err != nil {
			return err
		}
		if !a.Enabled {
			return errors.New("this account was disconnected before sign-in opened; try again")
		}
		if settled(a) {
			return nil
		}
		if a.LoginReady {
			break
		}
		if phase(a) == "Unavailable" {
			return errors.New("this account couldn't start sign-in; disconnect it and try again")
		}
		if waited >= e.signInTimeout {
			return errors.New("Tailscale hasn't supplied a sign-in page yet; check your internet connection and try again")
		}
		e.sleep(250 * time.Millisecond)
	}
	u, err := c.Login(ctx, a.ID)
	if err != nil {
		return err
	}
	if printURL {
		fmt.Fprintln(e.stdout, u)
		return nil
	}
	if err = e.open(u); err != nil {
		return errors.New("couldn't open the browser; rerun with --print-url")
	}
	fmt.Fprintf(e.stdout, "Finish signing in to %s in your browser, then check: tailmulti status\n", a.Name)
	return nil
}

func doctor(ctx context.Context, c controlclient.Client, e env, asJSON bool) error {
	if asJSON {
		return printRaw(ctx, c, e, "/v1/setup")
	}
	s, err := c.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.stdout, headline(s))
	if serviceBuild, err := c.ServiceBuild(ctx); err == nil {
		fmt.Fprintf(e.stdout, "Service build %s, command build %s\n", serviceBuild, build)
	}
	fmt.Fprintf(e.stdout, "%d accounts, %d devices, %d conflicts\n", len(s.Accounts), len(s.Devices), len(s.Conflicts))
	report, err := c.Setup(ctx)
	if err != nil {
		// A preview service has no host checks.
		fmt.Fprintln(e.stdout, "Host checks unavailable:", err)
		return nil
	}
	fmt.Fprintln(e.stdout)
	for _, check := range report.Checks {
		mark := "ok  "
		if check.State != "pass" {
			mark = "FAIL"
		}
		fmt.Fprintf(e.stdout, "%s  %s\n      %s\n", mark, check.Title, check.Detail)
	}
	if !report.Ready {
		return errors.New("this Mac isn't ready to connect")
	}
	return nil
}

func printRaw(ctx context.Context, c controlclient.Client, e env, endpoint string) error {
	raw, err := c.Raw(ctx, endpoint)
	if err != nil {
		return err
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		return err
	}
	return encode(e.stdout, value)
}

func encode(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func headline(s controlclient.Status) string {
	switch {
	case s.Mode == "active" && s.Health == "ready":
		return "Tailmulti is on."
	case s.Mode == "active":
		return "Tailmulti is on: " + s.Health
	case s.Mode == "inactive":
		return "Tailmulti is off. Turn it on with: tailmulti up"
	case s.Mode == "preview":
		return "Preview service: networking is not available."
	}
	return "Tailmulti mode: " + s.Mode
}

func printStatus(w io.Writer, s controlclient.Status) {
	fmt.Fprintln(w, headline(s))
	fmt.Fprintln(w)
	printAccounts(w, s)
	if len(s.Conflicts) > 0 {
		fmt.Fprintln(w, "\nConflicts:")
		for _, conflict := range s.Conflicts {
			fmt.Fprintln(w, "  "+conflict)
		}
	}
}

func printAccounts(w io.Writer, s controlclient.Status) {
	if len(s.Accounts) == 0 {
		fmt.Fprintln(w, "No accounts. Add one with: tailmulti account add NAME")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACCOUNT\tSTATE\tDEVICES\tID")
	for _, a := range s.Accounts {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", a.Name, phase(a), a.PeerCount, a.ID[:min(8, len(a.ID))])
	}
	tw.Flush()
}

func printDevices(w io.Writer, s controlclient.Status, only string, online, asJSON bool) error {
	names := map[string]string{}
	for _, a := range s.Accounts {
		names[a.ID] = a.Name
	}
	devices := []controlclient.Device{}
	for _, d := range s.Devices {
		if (only == "" || d.ProfileID == only) && (!online || d.Online) {
			devices = append(devices, d)
		}
	}
	sort.SliceStable(devices, func(i, j int) bool {
		if a, b := names[devices[i].ProfileID], names[devices[j].ProfileID]; a != b {
			return a < b
		}
		return devices[i].Name < devices[j].Name
	})
	if asJSON {
		return encode(w, devices)
	}
	if len(devices) == 0 {
		fmt.Fprintln(w, "No devices.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACCOUNT\tDEVICE\tADDRESSES\tONLINE")
	for _, d := range devices {
		state := "no"
		if d.Online {
			state = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", names[d.ProfileID], d.Name, strings.Join(d.Addresses, ", "), state)
	}
	return tw.Flush()
}

// phase mirrors the app's account labels.
func phase(a controlclient.Account) string {
	label := ""
	switch strings.ToLower(a.State) {
	case "running":
		return "Connected"
	case "needslogin":
		return "Sign-in required"
	case "needsmachineauth":
		return "Waiting for admin approval"
	case "starting", "nostate", "stopped", "disconnected":
		if a.Enabled {
			return "Connecting"
		}
		label = "Not connected"
	case "activation required":
		return "Needs network setup"
	case "unavailable", "inuse":
		return "Unavailable"
	default:
		return a.State
	}
	if !a.HasIdentity {
		return "Sign-in required"
	}
	return label
}

// resolve finds an account by name, then ID, then unique ID prefix.
func resolve(s controlclient.Status, ref string) (controlclient.Account, error) {
	for _, a := range s.Accounts {
		if strings.EqualFold(a.Name, ref) {
			return a, nil
		}
	}
	var matches []controlclient.Account
	lowered := strings.ToLower(ref)
	for _, a := range s.Accounts {
		if a.ID == lowered {
			return a, nil
		}
		if len(lowered) >= 4 && strings.HasPrefix(a.ID, lowered) {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return controlclient.Account{}, fmt.Errorf("no account named %q; list them with: tailmulti account list", ref)
	}
	return controlclient.Account{}, fmt.Errorf("%q matches more than one account ID", ref)
}

func quote(name string) string {
	if strings.ContainsAny(name, " \t'\"\\$`!*?()[]{}<>|&;~#") {
		return "'" + strings.ReplaceAll(name, "'", `'\''`) + "'"
	}
	return name
}
