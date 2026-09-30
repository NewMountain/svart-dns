# Svart DNS

What are we looking to solve:

1. I used PiHole (PH) for a long time. It was pretty good
2. I used AdGuard (AG) for a while, it was also pretty good

I never felt like either "got me" and so I'm making my own

## How I typically use it

The whole DNS resolution process is something I want to care about,
but honestly, I don't. I'm totally fine just pointing at Mulvad,
Quad 9, Cloudflare all 1s and calling it a day. I would like to
semi-randomly distribute that traffic so that correlating and
making sense of me is a touch harder. Bonus points for using
mullvad as now my DNS lookups are also spread across countries
which hopefully makes things even more annoying for data aggregators.
I have no delusions, if a letter agency ever wanted to go after me,
they'd crack it pretty quickly, but I have no chance against them
and, frankly, they have no reason to care about me. I'm
much more worried about data brokers, aggregators, my ISP, etc.
So let's make it maximally difficult for those assholes.

All DNS lookups should be https or something super encrypted
again, maximum difficulty for those assholes.

Ideally, I want to cache those DNS lookups in memory so that all subsequent
calls are lightning fast. Slow DNS is the worst, so for this application
I am actually VERY fixated on speed. This is the hottest part of my network
and when it's slow I get angry fast. I pay for super fast internet,
it should never feel slow because of DNS. For this reason, DNS pages out
to Sweden (Mullvad) can be a drag, so try to balance privacy, spread of data
and performance as best as you can. Once you make the call, store the
record in memory so we only pay that cost once. I realize all DNS
data has a TTL, so obviously mind that. As long as the TTL isn't insane
(like 60 seconds or less), honor it. I'm just trying to avoid constant
"pages out" to external DNS services by caching as aggressively with an
in memory solution as possible.

A lot of folks in Youtube Homelabbing gush about recusive DNS and,
while cool, from what I understand, it takes much longer, and as almost
all of it is done unencrypted on route 53, it actually is MORE of a privacy
leak than just making https DNS calls spread across providers. So... that's
a big minus. Unless you can do recursive DNS resolution over HTTPS or some
other encrypted format, I say pass. I'm not worried (at this time) about
censorship or quad 9/mullvad/etc messing with DNS records. If they do,
then we'll revisit the tradeoff of recursive resolution.

In terms of hardware, I have tons. If you need 2 gigs of RAM just for a hashmap,
you got it. Not a problem. I have no interest on making this raspberry
pi friendly. If you need a 2-4 core machine with 8GB of memory, just say
the word. I've got Hypervisor and threadrippers (plural) begging for a workload
to really work them. Having that said, speed and efficiency are two sides of
the same coin, you should not be stupid in resource usage. Be efficient
and effective, no wasted effort, no wasted resources.

To me, the more interesting point of differentiation is once we have the
lookup. This is really where PiHole and AdGuard fall on their face for me
even though this is supposedly their "bread and butter".

First, the UIs all kind of suck. I really want to spend a lot of time here,
but I will design a beautiful UI with Gemini later. For now, it can be ugly,
but functional. You should expect basically anything to be queryable, searchable,
filterable by just about any facet or feature and aggregations on any of those
cartesian options. PH/AG are really focussed on privacy. Which makes sense.
They stress how they barely have logs and while that's cool, I'm actually
interested in the analytics. I use the streams to figure out if someone
is spying on me, or if a server is leaking telemetry or if a user's device
is actually leaking things (or attempting to). I'm not being creepy
and spying on my family's internet use (frankly, I don't care), I'm
much more interested if a server or user device (or some sketch IOT thing)
is trying to do something I don't want and being able to figure that out
as quickly as possible is a killer use case for me. DNS has the unusual
property of exposing a lot of gross stuff and I have always found it
to be unreasonably affecting at identifying naughty software and hardware.
I would like this tool to make those sorts of patterns super obvious.

The second problem: blocklists. PH basically says Steven Black/FireBog/etc
AG says Hegezi, Dandelion and their own. But they sort of just raise their hands
and say "your problem". This really sucks. What I actually want is
all flavors (because annoyingly, all of these providers maintain 3-20 lists) of all
lists and then conduct an analysis on either a single domain or all my domains in a
period to see: what would using one list vs another do for me? Transparently,
right now, I have blocklist FOMO. I have a deeply uncomfortable sense that I'm not
blocking enough. I log into AdGuard see a block rate in the high teens and think
"damn, that seems low, surely I'm missing something". I would like to not feel that way
and have a tool to actually empirically review that. It would be much nicer to say
"I ran my/all/my wife's/multiple family members/etc traffic over the last week, here's
what each list would have done about it". I could review this manually, or feed it into
an LLM to tune the maximally restrictive level without bricking the entire internet.

On the topic of bricking the entire internet. My family has a nasty habit of going
around the DNS. My daughter will simply repoint her devices to 1.1.1.1 at the first
inconvenience. Thus presenting an enormous vulnerability surface. I don't blame her
she needs to access some weird proctoring spyware thing to take a school exam or
something. But again, PH and AG don't make this easy. I would much prefer identifying
all users of the network with a human name (10.42.1.118 is the NAS), ideally, it would
even get more granular than that to say which Docker container on the NAS, but certainly
IP level would identify Sophie's phone vs Chris' macbook, etc. Based on that we could say
Chris' macbook (for work) is subject to a lower level blocklist. He's not on the "general"
internet, so the risk is lower of sketchy stuff, but he needs segment.io to do his job.
As opposed to a Ubuntu server which should be absolutely redline levels of lockdown.
Basically block everything that isn't apt, python, docker, github, etc.

These tools make this _really_ hard to do and it would be nice to say this user or
this device is subject to this policy (the union of all the blocklists above) and, in
addition allow custom block/allow per user as well.

While per user is nice, it would be even better to have "groups". I don't want to manage
blocklists device by device. To be concrete: I don't want my wife complaining that Facebook
works on her phone but not computer or vice versa. It would be nice to create a Sam group
and then add all the various identities (again, her IPs or mac addresses as possible)
to that group so that her experince on the all devices is the same.

In terms of exception cascading, it should be big to small. Which is to say,
If a group denies but an IP allows, allow it. The smaller allow trumps the larger
block. Likewise if a group allows but a user blocks, block it.

I'm not sure what the level of granularity of DNS lookups is (whether you have Mac addresses)
or something like that. The use case there is if I'm running 30 docker containers on a box,
the situatuion always arises that all the boxes are super locked down but one has
some wacky unique snowflake requirement like AWS S3. If I whitelist AWS S3,
I want it to be for that one container, NOT ALL CONTAINERS ON THE NAS. I don't
know if this is possible, but it's something I'd really like and would
make me feel more comfortable.

Next pain point: I run a very elaborate network of DNS rewrites in my homelab.
Both AG and PH make this a very "pointy click" affair. I would like to easily
and quickly CRUD any DNS entry via API. I would put this in my Git action
runners so that any service could CRUD its own DNS entry quickly and easily.

On the topic of REST API, it's very odd to me that AG/PH APIs are so shitty.
Anything you can do the UI should be easily accomplished via API as well.

There should be a basic login to manage users and admins can create/administer API tokens
or users that can either read only or full blown admin.

On the topic of DevOps, right now I run one instance of Adguard which means
the second Hypervisor restarts, my network goes dark. I would very much like to
run an instance of this program on each Hypervisor node and then tell my router to use
either node so that I don't have downtime. While that's great for uptime, that creates
the problem of staying in sync. I would like the ability to make each instance
of this program aware of their peers on the network and for them to gossip to remain
in sync with each other. If I make a change in one instance (let's say apply a user whitelist),
it should propogate to all others within moments. It doesn't need to be realtime, but sub-second
in a homelab setting seems pretty achievable. Bonus points if the instances
can recognize if others are down.

Furthermore, on the topic of DevOps, I'd like you to emit as much data as possible to
my LGTM stack. It would be great to be able to also review DNS entries by user in Grafana
for example. Metrics, monitoring, logs, traces, etc would be delightful so that I could
build into the rest of my devops practice and integrated into the larger ecosystem
to make it easier to correlate bad actors, and general usage patterns.

I can't stress enough, I really want you to focus on the backend and the API,
while some sort of UI will be required, I expect what you produce to be pretty
mid and ugly, so just enough to validate and test the features, don't worry
too much about the aesthetics. I fully expect to conduct a concurrent design
session with Google Gemini which will likely result in a React SPA which will utilize
all of the wonderful APIs you have created.

Also, it should be super easy to integrate the log stream
into traditional LGTM stack to allow me to check both the health of the service

I never use DHCP settings and don't really want to ever manage DHCP
inside of this tool. That's something I push onto
OpnSense or Ubiquiti depending on who the router is and that works
fine for now.

## Turning the motivation into useful workflows

The founding notes above describe why Svart was built and the author's goals;
they are not a complete feature comparison or a promise of every proposed capability.
The current workflows are described in the [README](../README.md) and
[Overview](overview.md).

### Compare stricter lists against your own traffic

A block percentage alone does not explain what a stronger list would change.
Analysis shows which downloaded lists match a set of domains, including the
matching rules. Load recent domains observed on the network or one client,
or paste examples, and compare the lists before assigning them. This is list
matching against observed domains, not a complete replay of hypothetical policy.
The Policy Simulator explains the client's current range/group/device cascade.

### Give different devices the right policy

Apply a baseline to a source-IP subnet, shared rules to a group, and an exception
to an individual client IP. A device decision overrides its group, which overrides
the range. That supports stricter server rules and different work/family policies
without requiring a separate DNS server for each group. Pi-hole and AdGuard Home
also have per-client controls; Svart's focus is this explicit cascade together
with traffic-based list comparison and investigation.

### Explain and investigate queries that reach Svart

Logs include filtering decisions and matching rules. On Linux amd64, the
Investigation interface provides read-only SQL across recent logs and archived
history to explore device activity and unfamiliar domains. DNS traffic sent to
another resolver is outside Svart's visibility; detecting or preventing bypass
requires network controls or separate network telemetry.

### Keep the resolver responsive

The query path uses in-memory policy snapshots and caches; list comparison and
SQL investigation run on demand. The [benchmark evidence](benchmarks.md) records
throughput, tail latency, logging, and memory tradeoffs. It does not establish
universal parity or negligible overhead compared with other resolvers.
