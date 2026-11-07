# **settings**

```bash
dotdl config output ~/Videos         # default output folder
dotdl config cookies ~/cookies.txt   # cookies for every download
dotdl config cookies ""              # clear a setting
```

stored in `~/.config/dotdl/config.env`. environment variables, and a `.env`
in the current folder, take priority:

| variable | meaning | default |
|---|---|---|
| `DOTDL_OUTPUT_DIR` | output folder, and the root of a relative `-o` | current folder |
| `DOTDL_COOKIES_FILE` | netscape cookies file | none |
| `DOTDL_TEMP_DIR` | temporary files | the system's |
| `DOTDL_JS_RUNTIMES` | yt-dlp's javascript runtime, e.g. `node` | deno, else node |
| `DOTDL_LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |
