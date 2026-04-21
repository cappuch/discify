# discify

a lightweight web app that shows:
- what song ur currently playing on spotify
- the lyrics of the song
- (optional) typed lyrics - completely optional

by no means is this a proper functional app, it's mostly meant for OBS/streaming purposes, and is not meant to be used as a standalone app.

<img width="777" height="270" alt="{D8F76417-5613-4C38-9F31-FEA4227E3225}" src="https://github.com/user-attachments/assets/a95da6b9-29ac-411c-9ef5-1742c6ec59da" />
<img width="524" height="521" alt="{3D9C8791-B2AB-4E24-A58D-C02A508FC9DA}" src="https://github.com/user-attachments/assets/47d840a5-0290-4280-a27b-35f5da326409" />

## how to run
1. clone the repo
2. create a spotify app and set the redirect URI to `http://127.0.0.1:8080/callback` !! this is important !! (go to here: [https://developer.spotify.com/dashboard/applications](https://developer.spotify.com/dashboard/applications), create an app, then go to "edit settings" and add the redirect URI)
3. copy the content from `.env.example` to `.env` and fill in the values (client id and secret from the spotify app you created)
4. run `go run main.go`
5. open `http://localhost:8080` in your browser
6. click "connect to spotify" and follow the instructions
7. once one browser logs in, other clients on the same discify server (like OBS browser source) can use that authenticated session without logging in again

### routes:
- `/` - home page
- `/lyrics` - lyrics page
- `/typed_lyrics` - typed lyrics page

## config
config is stored in `config.json` in the root of the project. it has the following structure:
```json
{
    "art_mode": "vinyl", // "vinyl" or "picture"
}
```

## license
this project is licensed under the DBAFD license (my own varient of the mit license). see [license.md](license.md) for more details.

## future roadmap
- [ ] add support for other music platforms (apple music, youtube music, etc.) (as spotify's daniel ek is funding the war machine...)
- [ ] add a customizable UI (maybe using a templating engine or something)

## albums while coding
- [Ninajirachi - I Love My Computer](https://open.spotify.com/album/77CZUF57sYqgtznUe3OikQ)
- [Kanye West - TLOP](https://open.spotify.com/album/7gsWAHLeT0w7es6FofOXk1)
