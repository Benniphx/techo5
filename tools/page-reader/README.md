# Page reader

A headless Chromium for the voice assistant, when it answers [directly](../../docs/setup.md#voice-without-home-assistant).
Some pages have no text until their scripts run, like a store's hours, and some sites turn away a
plain request. The device reads those pages through this instead: it loads the page the way a
browser does and sends the finished page back, and the device takes the text from it as usual.
It is only asked when a page has no text as it comes.

## Run it

On any machine with Docker, in this folder:

```
docker build -t techo5-reader .
docker run -d --name reader --restart unless-stopped --cap-add NET_ADMIN \
  --sysctl net.ipv6.conf.all.disable_ipv6=1 --memory 1g --shm-size 512m \
  -p 8891:8891 techo5-reader
```

Then set **Page reader** on the setup page (**Sound & Voice**, **Voice assistant**) to
`http://<that machine>:8891`. `curl http://<that machine>:8891/health` answers `ok`.

## Keeping it off your network

A page the assistant reads was found on the web, and a page can send a browser anywhere: to your
router's page, or another device at home. So the container starts with a firewall (`entrypoint.sh`)
that lets the browser reach the internet and the DNS server only, never private addresses
(192.168.x.y, 10.x, 172.16-31.x), the shared 100.64.0.0/10 range, or the machine itself. That is what
`--cap-add NET_ADMIN` is for: the firewall is set as root, and then the server runs as an ordinary
user that cannot change it. IPv6 is turned off in the container.

The reader has no password, so keep its port on your own network, as you would the search server.

It uses about 100 MB of memory while idle, more while a page loads (up to the 1 GB set above), and
a few seconds per page. Two pages are read at once at most; a third gets "busy".
