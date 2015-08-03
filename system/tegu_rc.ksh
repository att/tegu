#! /bin/sh
# vi: sw=4 ts=4:
#
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
# ---------------------------------------------------------------------------
#

# Mnemonic:	tegu.rc.ksh
# Abstract: This script is installed (copied) to /etc/init.d/tegu and then the command
#			'insserv /etc/init.d/tegu' is run to add it to the list of things that are
#			automatically started when system enters run level 2 through 5. The script
#			starts BOTH tegu and the agent (5 instances). This script assumes that both
#			tegu and agent binaries are in the PATH.
#
# Usage:	service tegu {start|stop|standby}
#
#			CAUTION: this script assumes that the user ID created for tegu is 'tegu'.
#				If a different uer id was used the value must be changed just below
#				this header box. Regardless of the user ID created for tegu, the
#				directory in /etc is assumed to be tegu (all tegu scripts make that
#				assumption!)
#
# Date:		20 May 2014
# Author:	E. Scott Daniels
#
# Mods:		27 Aug 2014 - Now passes the second command line parameter to tegu_start
#			01 Dec 2014 - Added restart command; ensure environment is set for failover
#					support when running 'service tegu standby'.
#			14 Dec 2014 - Renamed restart to reload since service buggers restart.
#			18 Dec 2014 - Ignore signal 15 to prevent kill from killing us.
#			30 Jan 2015 - Added start of ha daemon.
#			02 Feb 2015 - Changed start to start only the ha daemon which will start tegu
#					if needed on this host. Added forceup option to allow tegu to be forced
#					to start without the ha daemon.
#			20 Feb 2015 - Added -u option to killall to supress warnings from killall
#			10 Mar 2015 - Corrected missing tegu_user on the ha start in standby.
#			09 Apr 2015 - Corrected typo in forcedown logic.
#----------------------------------------------------------------------------------------
trap "" 15				# prevent killall from killing the script when run from service

tegu_user=tegu			#### change this if a different user name was setup for tegu
tegu_group=tegu			#### change this if a different group name was setup for tegu

### BEGIN INIT INFO
# Provides:			tegu
# Required-Start:	
# Required-Stop:	0 1 6
# Default-Start:	2 3 4 5
# Default-Stop:		0 1 6
# Short-Description: Tegu bandwidth reservation manager
### END INIT INFO

set -e

# /etc/init.d/tegu: start and stop Tegu bandwidth reservation maanger

test -x /usr/bin/tegu || exit 0
test -x /usr/bin/start_tegu || exit 0
test -x /usr/bin/start_tegu_agent || exit 0
test -x /usr/bin/tegu_agent || exit 0

if test ! -d /var/log/tegu
then
	mkdir /var/log/tegu
fi
chown $tegu_user:$tegu_group /var/log/tegu	# always ensure that the ownership is correct

if test ! -d /var/lib/tegu
then
	mkdir /var/lib/tegu
fi
chown $tegu_user:$tegu_group /var/lib/tegu

if test -d /etc/tegu
then
	chown $tegu_user:$tegu_group /etc/tegu
	chown $tegu_user:$tegu_group /etc/tegu/*
	chmod 755 /etc/tegu
	chmod 600 /etc/tegu/tegu.cfg
fi

umask 022

if ! test -f /etc/tegu/tegu.cfg
then
	exit 0
fi

export PATH="${PATH:+$PATH:}/usr/bin:/usr/sbin:/sbin"	# ensure key directories are there

case "$1" in
  forceup)												# forces tegu to be started; might be stopped immediately by ha daemon
	su -c "PATH=$PATH start_tegu" $tegu_user
	su -c "PATH=$PATH start_tegu_agent 1 2 3 4 5" $tegu_user
	;;

  forcedown)											# force tegu and agents down; ha might well restart them
	set +e
	su -c "killall -u $tegu_user tegu_agent"
	su -c "killall -u $tegu_user tegu"
	;;

  start)
	su -c "PATH=$PATH start_tegu_ha" $tegu_user			# start high avail daemon; let it decided if Tegu should be running here
	;;

  stop)								# stop everything, including the ha process
	set +e							# don't exit if either fail (which they will if tegu not running)
	ha_pid="$(ps aux | grep "[p]ython.*tegu_ha" | awk '{ print $2 }' )"			# get the pid of the ha process
	if test -n "$ha_pid"
	then
		su -c "kill -9 $ha_pid" tegu		# python seems to ignore term signals, send it down hard
	fi

	su -c "killall -u $tegu_user tegu_agent" $tegu_user
	su -c "killall -u $tegu_user tegu" $tegu_user
	;;

  standby)
	if test ! -f /etc/tegu/active
	then
		touch /etc/tegu/standby	
		chown $tegu_user:$tegu_group /etc/tegu/standby	
		su -c "PATH=$PATH start_tegu" tegu >/dev/null 2>&1		# this will fail, but we want to ensure environment (cron etc.) is setup
	fi
	su -c "PATH=$PATH start_tegu_ha" $tegu_user					# start high avail daemon
	;;

  reload)
	set +e
	su -c "killall -u $tegu_user tegu_agent"
	su -c "killall -u $tegu_user tegu"
	
	# tegu_ha will restart things (safe to run this even if tegu_ha is already running)
	su -c "PATH=$PATH start_tegu_ha" tegu					# start high avail daemon; let it decided if Tegu should be running here
	;;


  status)
	/usr/bin/tegu_req ping|grep -q OK		# exit with non-zero if not running
	exit $?
	;;

  *)
	echo "Usage: $0 {start|stop|restart}"
	exit 1
esac

exit 0
