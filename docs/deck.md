# TECHO5 Deck

TECHO5 Deck turns an Echo Show into a stream deck: pages of touch buttons that switch OBS scenes,
start and stop the stream and the recording, mute your microphone and show or hide sources, and
that press shortcuts, type, open apps and websites and run scripts on your computer. The buttons
talk to OBS and the computer directly, so they work without Home Assistant, and they show what OBS
is doing: the live scene lit, the stream button lit while you're live, a muted input crossed out.

Show only (the Echo Show 5 and Show 8). OBS 28 and later has what the OBS buttons need built in;
the computer buttons need the small TECHO5 Deck agent on the computer
([below](#computer-buttons)).

## Set up OBS

In OBS, open **Tools → WebSocket Server Settings**:

1. Turn on **Enable WebSocket server**.
2. Leave **Enable Authentication** on, and note the password (**Show Connect Info** shows it).
3. Note the port: 4455 unless you changed it.

Your computer's firewall has to let the Show reach that port. Windows asks the first time OBS
listens; answer **Allow** for private networks.

## Set up the deck

On the device's setup page, **Screen & Photos → Deck**:

1. **OBS address**: your computer's address, like `192.168.1.20` (add `:4456` if you changed the
   port). Then the password, and **Save**. The line under it says **Connected** once OBS answers.
2. **Buttons across** and **Rows**: 4 by 3 to start with; up to 6 by 4.
3. **Page 1**: for each button, an **action** and what it acts on:
   - **Switch scene**: the scene, in **Scene or input**. The box suggests OBS's own scene names
     while it's connected.
   - **Stream on/off** and **Record on/off**: nothing more.
   - **Mute/unmute**: the input (your microphone, Desktop Audio) in **Scene or input**.
   - **Show/hide a source**: the scene in **Scene or input**, and the source in **Source**.

   A **label**, a **color** and an **icon** are optional; each action has its own. Icons are
   [Material Design icon](https://pictogrammers.com/library/mdi/) names, like `microphone-off`.
   **Save page**, and **Add a page** for more.

## Use it

- **Open it** with a swipe up from the bottom edge of the clock: start right at the bottom of the
  screen. A swipe up anywhere else is still the volume. You can also set **Tap on the clock** to
  **Deck** (setup page, Screen & Photos, or Settings → Display on the device).
- **Press** a button. It dims while the press goes out and turns red for a moment if it failed.
- **Swipe left and right** for the pages, and **down** to put the deck away. It stays up until you
  do.
- When OBS isn't running, the buttons gray out and the foot of the deck says why.

Home Assistant can press a button too, with the `deck_press` action
([Actions](actions.md#press-a-deck-button)).

## Computer buttons

Buttons can press a shortcut or a media key, type text, open an app or a website, or run a script
on a computer. That needs the **TECHO5 Deck agent** running there: one program, nothing to install.
Windows for now; macOS and Linux are next.

1. Download `techo5-deck.exe` from the release and run it. A window opens with the computer's name
   and a **pairing key** of 32 letters and digits. The first time, Windows asks whether to let it
   on the network: allow it on **private networks**.
2. On the Show's setup page, **Screen & Photos → Deck → Computers**: **Look for computers**, pick
   yours (or type its address), type the key, and **Pair**. It says **Connected** with how many
   apps and scripts it found.
3. On a page, give a button a **Computer** action and pick the computer:
   - **Press keys**: a shortcut the way you'd write it, `ctrl+shift+m`, `alt+f4`, `win+d`, `f13`,
     or a media key: `media_play_pause`, `media_next`, `media_previous`, `volume_up`,
     `volume_down`, `volume_mute`.
   - **Type text**: the words to type, as they are.
   - **Open app or website**: an app from the Start menu by its name (the box suggests them), or an
     `https://` address.
   - **Run script**: a name from the agent's script list (below).

Keep the agent's window open while you use the deck. To have it start when you sign in, run
`techo5-deck.exe -startup on` once (`-startup off` undoes it).

### The script list

Scripts are listed on the computer, never on the Show: a paired Show can run only what's in the
list, and only somebody at the computer can change it. The list is `scripts.txt` in the agent's
folder (`%APPDATA%\TECHO5 Deck`), one per line, a name, `=`, and the command:

```
Backup = robocopy "C:\Users\me\Documents" "D:\Backup\Documents" /MIR
Lights = "C:\Tools\lights.exe" --scene evening
```

Save it, then **Refresh** on the setup page (the Show also asks again every minute).

### Good to know about the agent

- It takes connections from your local network only, and only from a Show that has its key: the
  connection is encrypted with that key, and a wrong key gets nothing.
- `techo5-deck.exe -new-key` makes a new key; every paired Show then has to pair again.
- Windows won't let it press keys into a window that's running as administrator, or while the
  screen is locked. The button turns red and the agent's window says why.

## Good to know

- OBS's WebSocket isn't encrypted. The password itself is never sent (OBS uses a challenge), but
  scene names and commands are readable on your network. That's fine at home; don't point the deck
  at an OBS across the internet.
- The password is kept on the device. The setup page only says whether one is saved.
- The agent's key and the OBS password are kept on the Show; the setup page only says whether
  they're set.
