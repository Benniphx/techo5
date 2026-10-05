# TECHO5 Deck

TECHO5 Deck turns an Echo Show into a stream deck: pages of touch buttons that switch OBS scenes,
start and stop the stream and the recording, mute your microphone and show or hide sources. The
buttons talk to OBS Studio directly, so they work without Home Assistant, and they show what OBS is
doing: the live scene lit, the stream button lit while you're live, a muted input crossed out.

Show only (the Echo Show 5 and Show 8). Nothing to install on the computer: OBS 28 and later has
what the deck needs built in.

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

## Good to know

- OBS's WebSocket isn't encrypted. The password itself is never sent (OBS uses a challenge), but
  scene names and commands are readable on your network. That's fine at home; don't point the deck
  at an OBS across the internet.
- The password is kept on the device. The setup page only says whether one is saved.
- Keyboard shortcuts and launching apps on the computer need the TECHO5 Deck agent, which is coming
  next ([the plan](deck-plan.md)).
