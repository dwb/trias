# Trias

Sets up an SSH tunnel, a HTTP(S) proxy that connects through that tunnel, and starts a sub-shell with the environment set such that HTTP clients will use the proxy. You don't have to worry about local port numbers or anything.

Start with a profile name, like:

```
trias staging
```

where before you've set this profile up in its config file at `~/.config/trias.json`:

```json
{
  "profiles": {
    "staging": {
      "host": "10.1.1.1",
      "user": "joebloggs"
    }
  }
}
```

An empty user will use your local user name, as standard `ssh`.

The sub-shell will be started with `TRIAS_PROFILE` set to the name of the profile used, so your shell startup script could pick this up and make your prompt fancy or something.
