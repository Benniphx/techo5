#!/bin/sh
# Runs as root only to set the firewall: the browser may reach the internet and the DNS server, never
# this house's network, the shared 100.64.0.0/10 range, or the machine it runs on. Then the server runs as pwuser, which
# cannot change the rules.
set -e
ns=$(awk '/^nameserver/ {print $2; exit}' /etc/resolv.conf)
iptables -F OUTPUT
iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
if [ -n "$ns" ]; then
	iptables -A OUTPUT -d "$ns" -p udp --dport 53 -j ACCEPT
	iptables -A OUTPUT -d "$ns" -p tcp --dport 53 -j ACCEPT
fi
for n in 0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.0.0.0/24 \
	192.168.0.0/16 198.18.0.0/15 224.0.0.0/4 240.0.0.0/4; do
	iptables -A OUTPUT -d "$n" -j REJECT
done
ip6tables -P OUTPUT DROP 2>/dev/null || true
exec setpriv --reuid=pwuser --regid=pwuser --init-groups --inh-caps=-all --bounding-set=-all node /app/server.js
