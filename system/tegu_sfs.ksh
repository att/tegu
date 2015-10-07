#!/usr/bin/env ksh
# ---------------------------------------------------------------------------
#   Copyright (c) 2013-2015 AT&T Intellectual Property
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at:
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
# ----------------------------------------------------------------------------------------
#
#	Mnemonic:	sfs - Scalable flow steering
#	Abstract: 	This script takes a data file outlining a scalable flow-steering request
#				and generates a custom script to implement that request, including OVS
#				statements to both add and remove the flows from each affected physical
#				node, as well as a section containing neutron port-update commands to allow
#				middleboxes to spoof IP addresses.
#  
#	Date:		10 Jul 2015
#	Author:		Robert Eby
#
# ----------------------------------------------------------------------------------------

PATH=/bin:/usr/bin:/tmp/tegu_b
PFX=/tmp/sfs.$$.		# prefix for all temp files
LISTFILE=${PFX}..list	# list of all temp files
priority=600			# priority should be higher than bandwidth fmods (501),
			# and must be lower than our initial priority 900 flow-mod
			# right now it is fixed
lfm_timeout=300			# timeout for learned flow-mods
fmod_timeout=300		# the timeout for the general rule that causes steering
verbose=trace

# ------------------------- Function Definitions -------------------------

function trace
{
	echo + $* 1>&2
}

# Given the name of a middlebox set, do the indirection to get the MAC list
function macaddrlist
{
	t='$'"$1"
	eval echo $t
}

# Look in the MAC -> port map to get the port number for a MAC
function mac2port
{
	echo $SFS_MACMAP | tr ' ' '\012' | grep "^$1/" | cut -d/ -f4
}

# Look in the MAC -> phys map to get the physical host for a MAC
function physhost
{
	echo $SFS_MACMAP | tr ' ' '\012' | grep "^$1/" | cut -d/ -f3
}

# Given a MAC, find the UUID of the port that goes with it
function getportuuid
{
	echo $SFS_MACMAP | tr ' ' '\012' | grep "^$1/" | cut -d/ -f2
}

#
#  Generate the flow rules needed to go from port A to ports B(1), B(2), ... B(n)
#  inport and physhost refer to the port on the switch and the physical machine where port A is located
#
#      +-- B(1)
#  A --+-- B(2)
#      +-- B(n)
#
function genOutboundFlows
{
	$verbose genOutboundFlows $*
	inport=$1
	physhost=$2
	macs="$3"
	nmbox=0
	preamble=
	[ -s $PFX$physhost ] || preamble=1

	(
		if [ -n "$preamble" ]
		then
			echo "${hard_to}table=89,cookie=0xabcd,$SFS_RULES,priority=0,action=write_metadata:0x08/0x08"
			## echo "${hard_to}cookie=0xabcd,tcp_flags=+syn,$SFS_RULES,metadata=0x00/0x0f,priority=901,action=set_field:0x1->metadata,resubmit(,98),resubmit(,89),resubmit(,0)"
			echo "${hard_to}cookie=0xabcd,$SFS_RULES,metadata=0x00/0x0f,priority=900,action=set_field:0x1->metadata,resubmit(,89),resubmit(,0)"
		fi
		for mac in $macs
		do
			cmac=$(echo $mac | tr -d : )
			learn="learn(cookie=0xfade,table=89,priority=$priority,idle_timeout=300,hard_timeout=300,in_port=$inport,$SFS_RULES,NXM_OF_TCP_SRC[],load:0x$cmac->NXM_OF_ETH_DST[],load:0xff->NXM_NX_REG0[])"
			echo "${hard_to}table=99,cookie=0xdaff,in_port=$inport,$SFS_RULES,reg0=$nmbox,priority=$priority,action=$learn,mod_dl_dst:$mac"
			nmbox=$(( nmbox + 1 ))
		done
		if (( nmbox != 0 ))
		then
			echo "${hard_to}cookie=0xabcd,in_port=$inport,$SFS_RULES,metadata=0x09/0x0f,priority=600,action=set_field:0x02->metadata,multipath(symmetric_l4,1024,hrw,$nmbox,0,NXM_NX_REG0[]),resubmit(,99),resubmit(,0)"
		fi
	) >> $PFX$physhost
	echo $PFX$physhost >> $LISTFILE
}

#
#  Generate the flow rules needed to do the return flow from port Z to ports B(1), B(2), ... B(n)
#  inport and physhost refer to the port on the switch and the physical machine where port Z is located
#
#  B(1) --+
#  B(2) --+-- Z
#  B(n) --+
#
function genReturnFlows
{
	$verbose genReturnFlows $*
	inport=$1
	physhost=$2
	nmbox=0
	preamble=
	(
		echo "${hard_to}cookie=0xabcd,tcp_flags=+syn,$SFS_RULES,metadata=0x00/0x0f,priority=901,action=set_field:0x1->metadata,resubmit(,98),resubmit(,0)"
		echo "${hard_to}cookie=0xabcd,in_port=$inport,$SFS_REV_RULES,metadata=0x00/0x0f,priority=900,action=set_field:0x1->metadata,resubmit(,89),resubmit(,0)"
		echo "table=98,cookie=0xdada,$SFS_RULES,priority=600,action=learn(cookie=0xfade,table=89,priority=600,idle_timeout=300,hard_timeout=300,in_port=$inport,$SFS_REV_RULES,NXM_OF_TCP_DST[]=NXM_OF_TCP_SRC[],load:NXM_OF_ETH_SRC[]->NXM_OF_ETH_DST[],load:0xff->NXM_NX_REG0[])"
	) >> $PFX$physhost
	echo $PFX$physhost >> $LISTFILE
}

#
#  Generate the flow rules needed for a middlebox, where there are both "forward" flows
#  from port A to ports B(1), B(2), ... B(n), and return flows from port A to ports Q(1), Q(2), ... Q(n)
#
#  Q(1) --+       +-- B(1)
#  Q(2) --+-- A --+-- B(2)
#  Q(n) --+       +-- B(n)
#
function genMiddleBoxFlows
{
	$verbose genMiddleBoxFlows $*
	inport=$1
	physhost=$2
	macs="$3"
	nmbox=0
	preamble=
	[ -s $PFX$physhost ] || preamble=1

	(
		if [ -n "$preamble" ]
		then
			echo "${hard_to}table=89,cookie=0xabcd,$SFS_RULES,priority=0,action=write_metadata:0x08/0x08"
			##  echo "${hard_to}cookie=0xabcd,tcp_flags=+syn,$SFS_RULES,metadata=0x00/0x0f,priority=901,action=set_field:0x1->metadata,resubmit(,98),resubmit(,89),resubmit(,0)"
			echo "${hard_to}cookie=0xabcd,$SFS_RULES,metadata=0x00/0x0f,priority=900,action=set_field:0x1->metadata,resubmit(,89),resubmit(,0)"
		fi
		echo "${hard_to}cookie=0xabcd,in_port=$inport,$SFS_REV_RULES,metadata=0x00/0x0f,priority=900,action=set_field:0x1->metadata,resubmit(,89),resubmit(,0)"
		for mac in $macs
		do
			cmac=$(echo $mac | tr -d : )
			learn="learn(cookie=0xfade,table=89,priority=$priority,idle_timeout=300,hard_timeout=300,in_port=$inport,$SFS_RULES,NXM_OF_TCP_SRC[],load:0x$cmac->NXM_OF_ETH_DST[],load:0xff->NXM_NX_REG0[])"
			learn2="learn(cookie=0xfade,table=89,priority=$priority,idle_timeout=300,hard_timeout=300,in_port=$inport,$SFS_REV_RULES,NXM_OF_TCP_DST[]=NXM_OF_TCP_SRC[],load:NXM_OF_ETH_SRC[]->NXM_OF_ETH_DST[],load:0xff->NXM_NX_REG0[])"
			echo "${hard_to}table=99,cookie=0xdaff,in_port=$inport,$SFS_RULES,reg0=$nmbox,priority=$priority,action=$learn,$learn2,mod_dl_dst:$mac"
			nmbox=$(( nmbox + 1 ))
		done
		if (( nmbox != 0 ))
		then
			echo "${hard_to}cookie=0xabcd,in_port=$inport,$SFS_RULES,metadata=0x09/0x0f,priority=600,action=set_field:0x02->metadata,multipath(symmetric_l4,1024,hrw,$nmbox,0,NXM_NX_REG0[]),resubmit(,99),resubmit(,0)"
		fi
	) >> $PFX$physhost
	echo $PFX$physhost >> $LISTFILE
}

function genMiddleBoxFlowsOLD
{
	$verbose genMiddleBoxFlows $*
	inport=$1
	physhost=$2
	macs="$3"
	nmbox=0
	preamble=
	[ -s $PFX$physhost ] || preamble=1

	(
		if [ -n "$preamble" ]
		then
			echo "${hard_to}table=89,cookie=0xabcd,$SFS_RULES,priority=0,action=write_metadata:0x08/0x08"
			echo "${hard_to}cookie=0xabcd,tcp_flags=+syn,$SFS_RULES,metadata=0x00/0x0f,priority=901,action=set_field:0x1->metadata,resubmit(,98),resubmit(,89),resubmit(,0)"
			echo "${hard_to}cookie=0xabcd,$SFS_RULES,metadata=0x00/0x0f,priority=900,action=set_field:0x1->metadata,resubmit(,89),resubmit(,0)"
		fi
		echo "${hard_to}cookie=0xabcd,in_port=$inport,$SFS_REV_RULES,metadata=0x00/0x0f,priority=900,action=set_field:0x1->metadata,resubmit(,89),resubmit(,0)"
		echo "table=98,cookie=0xdada,$SFS_RULES,priority=600,action=learn(cookie=0xfade,table=89,priority=600,idle_timeout=300,hard_timeout=300,in_port=$inport,$SFS_REV_RULES,NXM_OF_TCP_DST[]=NXM_OF_TCP_SRC[],load:NXM_OF_ETH_SRC[]->NXM_OF_ETH_DST[],load:0xff->NXM_NX_REG0[])"
		for mac in $macs
		do
			cmac=$(echo $mac | tr -d : )
			learn="learn(cookie=0xfade,table=89,priority=$priority,idle_timeout=300,hard_timeout=300,in_port=$inport,$SFS_RULES,NXM_OF_TCP_SRC[],load:0x$cmac->NXM_OF_ETH_DST[],load:0xff->NXM_NX_REG0[])"
			echo "${hard_to}table=99,cookie=0xdaff,in_port=$inport,$SFS_RULES,reg0=$nmbox,priority=$priority,action=$learn,mod_dl_dst:$mac"
			nmbox=$(( nmbox + 1 ))
		done
		if (( nmbox != 0 ))
		then
			echo "${hard_to}cookie=0xabcd,in_port=$inport,$SFS_RULES,metadata=0x09/0x0f,priority=600,action=set_field:0x02->metadata,multipath(symmetric_l4,1024,hrw,$nmbox,0,NXM_NX_REG0[]),resubmit(,99),resubmit(,0)"
		fi
	) >> $PFX$physhost
	echo $PFX$physhost >> $LISTFILE
}

function genflows
{
	$verbose genflows $*
	arr=($*)
	n=${#arr[*]}
	n1=$(( $n - 1 ))
	i=0
	while [[ $i -lt $n ]]
	do
		if [[ $i -eq 0 ]]
		then
			cur=${arr[0]}
			nxt=${arr[1]}
			maclist="$(macaddrlist $nxt)"
			for mac in $(macaddrlist $cur)
			do
				port=$(mac2port $mac)
				phost=$(physhost $mac)
				genOutboundFlows $port $phost "$maclist"
			done
		elif [[ $i -eq $n1 ]]
		then
			cur=${arr[i]}
			for mac in $(macaddrlist $cur)
			do
				port=$(mac2port $mac)
				phost=$(physhost $mac)
				genReturnFlows $port $phost
			done
		else
			cur=${arr[i]}
			nxt=${arr[i+1]}
			maclist="$(macaddrlist $nxt)"
			for mac in $(macaddrlist $cur)
			do
				port=$(mac2port $mac)
				phost=$(physhost $mac)
				genMiddleBoxFlows $port $phost "$maclist"
			done
		fi
		i=$((i + 1))
	done
}

# Generate all the flow statements for one direction ($1 => $2)
# using middlebox sets in the order specified in $3
function gen1wayflows
{
	$verbose gen1wayflows $*
	inset="$1"
	mbsets="$2"
	prevset=
	# Note: assume we don't have to steer the last hop (from the last set)
	# since the switch should handle this using normal forwarding.
	for set in $inset $mbsets
	do
		if [ -n "$prevset" ]
		then
			# Intermediate flows from mbox(i) to mbox(i+1)
			maclist="$(macaddrlist $set)"
			for prevmac in $(macaddrlist $prevset)
			do
				port=$(mac2port $prevmac)
				phost=$(physhost $prevmac)
				genOutboundFlows $port $phost "$maclist"
			done
		fi
		prevset="$set"
	done
}

# ------------------------- Main code -------------------------
if [ $# != 1 -o ! -r "$1" ]
then
	echo "usage: sfs <environmentfile>"
	exit 1
fi
source "$1"
CHAIN=$(echo $1 | sed -e 's/.*tegu.sfs.//' -e 's/.data//' )
rm -f ${PFX}*

# Check input
if [ -z "$SFS_RULES" ]
then
	echo "Need to set $SFS_RULES in the input file."
	exit 1
fi

# Generate flow statements into separate files (one per phost)
if [ "$SFS_ONEWAY" == "true" ]
then
	gen1wayflows $SFS_SRCSET "$SFS_SETS"
else
	genflows $SFS_SRCSET $SFS_SETS $SFS_DSTSET
fi
# Generate the reverse direction (unless $SFS_ONEWAY is not true)
#	revsets=$(echo $SFS_SETS | tr ' ' '\012' | tac | tr '\012' ' ')
#	gen1wayflows $SFS_DSTSET "$revsets"
# fi
sort -u -o $LISTFILE $LISTFILE

hard_to=''
if [ -n "$SFS_EXPIRATION" ]
then
	hard_to="hard_timeout=$SFS_EXPIRATION,"
fi

#
# Combine all of the partial scripts into one master script that can be copied and executed on all machines.
#
mboxes=
nl='
'
for set in $SFS_SETS
do
	mboxes="${mboxes}#    ${set}='$(macaddrlist $set)'${nl}"
done
mboxes="${mboxes}#"
pnodes=$( sort -u $LISTFILE | sed "s;$PFX;;" | tr '\012' ' ' )
SFS_SRCMAC=$(macaddrlist $(macaddrlist SFS_SRCSET))
SFS_DSTMAC=$(macaddrlist $(macaddrlist SFS_DSTSET))
hostfrom=$(physhost $SFS_SRCMAC )
hostto=$(  physhost $SFS_DSTMAC )

cat <<EOF
#!/bin/ksh
#
# Plan file for chain: $CHAIN
#
# Tegu generated custom flow steering script for:
#    $SFS_SRCMAC (on $hostfrom) ==> $SFS_DSTMAC (on $hostto)
# using middlebox sets:
$mboxes
# This script must be run with a 'start' or 'stop' argument to add or remove the flows from br-int
# It should be run on the following physical nodes:
#	$pnodes
#
PATH=/bin:/usr/bin
HOSTS="$pnodes"
if [[ "\$1" != "start" && "\$1" != "stop" ]]
then
	echo "usage: \$0 [ start | stop ]"
	exit 1
fi
myid=\$(id -nu)
if [[ "\$myid" != "tegu" ]]
then
	echo "This script must run as the 'tegu' user!"
	exit 1
fi
me=\$(uname -n)
EOF
for i in $(cat $LISTFILE)
do
	pnode=$(echo $i | sed "s;$PFX;;" )
	echo
	echo "# ---------- Add flow statements on $pnode ----------"
	echo 'if [[ "$me" == "'$pnode'" && "$1" == "start" ]]'
	echo then
	echo sudo ovs-vsctl set bridge br-int protocols=OpenFlow10,OpenFlow11,OpenFlow12,OpenFlow13
	echo sudo ovs-ofctl -O OpenFlow10,OpenFlow11,OpenFlow12,OpenFlow13 add-flow br-int - '<<EOF'
	sed 's/.*br-int //' < $i
	echo EOF
	echo "echo Added flow rules to $pnode for $CHAIN at \$(date) | tee -a /tmp/tegu_sfs.log"
	echo fi
	echo

	echo "# ---------- Remove flow statements from $pnode ----------"
	echo 'if [[ "$me" == "'$pnode'" && "$1" == "stop" ]]'
	echo then
	echo sudo ovs-vsctl set bridge br-int protocols=OpenFlow10,OpenFlow11,OpenFlow12,OpenFlow13
	echo sudo ovs-ofctl -O OpenFlow10,OpenFlow11,OpenFlow12,OpenFlow13 del-flows br-int - '<<EOF'
	sed -e 's/cookie=0x....,//' -e 's/,action=.*//' -e "s/${hard_to}//" -e "s/,priority=.*//" < $i
	echo EOF
	echo "echo Removed flow rules from $pnode for $CHAIN at \$(date) | tee -a /tmp/tegu_sfs.log"
	echo fi
done
rm $(cat $LISTFILE) $LISTFILE
exit 0
