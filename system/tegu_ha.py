#!/usr/bin/env python
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

'''	
        Mnemonic:   tegu_ha.py
        Abstract:   High availability script for tegu. Pings other tegu's in the
                    standby_list. If no other tegu is running, then makes the
                    current tegu active. If it finds multiple tegu running, then
                    determines whether current tegu should be shut down or not based
                    on db timestamps. Assumes that db checkpoints from other tegus
                    do not override our checkpoints

        Date:       15 December 2015
        Author:     Kaustubh Joshi
        Mod:        2014 15 Dec - Created script
                    2015 30 Jan - Minor fixes.
                    2015 04 Feb - Change to ensure that the stanby file is always
                        there when tegu isn't running on the current host. (needed
                        only for monitoring at this point).
                    2015 06 Aug - Adjusted retry to offset response time delays.
                    2015 11 Aug - Added ability to recognise all host aliases for
                        this machine.
 ------------------------------------------------------------------------------

  Algorithm
 -----------
 The tegu_ha script runs on each tegu node in a continuous loop checking for
 hearbeats from all the tegu nodes found in the /etc/tegu/standby_list once
 every 5 secs (default). A heartbeat is obtained by invoking the "tegu ping"
 command via the HTTP API. If exactly one tegu is running, the script does
 nothing. If no tegu is running, then the local tegu is activated after
 waiting for 5*priority seconds to let a higher priority tegu take over
 first. A tegu node's priority is determined by its order in the standby_list
 file. If the current node's tegu is running and another tegu is found, then a
 conflict resolution process is invoked whereby the last modified timestamps
 from checkpoint files from both tegu's are compared, and the tegu with the
 older checkpoint is deactivated. The rationale is that the F5 only keeps a
 single tegu active at a time, so the tegu with the most recent checkpoint
 ought to be the active one.'''

import sys
import time
import os
import socket
import subprocess
import string

# Directory locations
TEGU_ROOT = os.getenv('TEGU_ROOT', '/var')              # Tegu root dir
LIBDIR = os.getenv('TEGU_LIBD', TEGU_ROOT+'/lib/tegu')
LOGDIR = os.getenv('TEGU_LOGD', TEGU_ROOT+'/log/tegu')
ETCDIR = os.getenv('TEGU_ETCD', '/etc/tegu')
CKPTDIR = os.getenv('TEGU_CKPTD', LIBDIR+'/chkpt')      # Checkpoints directory
LOGFILE = LOGDIR+'/tegu_ha.log'				            # log file for this script

# Tegu config parameters
TEGU_PORT = os.getenv('TEGU_PORT', 29444)		     # tegu's api listen port
TEGU_USER = os.getenv('TEGU_USER', 'tegu')           # run only under tegu user
TEGU_PROTO = 'http'

SSH_CMD = 'ssh -o StrictHostKeyChecking=no %s@%s '

RETRY_COUNT = 3      # How many times to retry ping command
CONNECT_TIMEOUT = 3  # Ping timeout

DEACTIVATE_CMD = '/usr/bin/tegu_standby on;' \
    'killall tegu >/dev/null 2>&1; killall tegu_agent >/dev/null 2>&1'  # Command to kill tegu
ACTIVATE_CMD = '/usr/bin/tegu_standby off;' \
    '/usr/bin/start_tegu; /usr/bin/start_tegu_agent' # Command to start tegu

# Command to force checkpoint synchronization
SYNC_CMD = '/usr/bin/tegu_synch'

# HA Configuration
HEARTBEAT_SEC = 5                    # Heartbeat interval in seconds
PRI_WAIT_SEC = 5                     # Backoff to let higher prio tegu take over
STDBY_LIST = ETCDIR + '/standby_list' # list of other hosts that might run tegu

# if present then this is a standby machine and we don't start
STDBY_FILE = ETCDIR + '/standby'
VERBOSE = 0

def logit(msg):
    '''Log error message on stdout with timestamp'''
    now = time.gmtime()
    sys.stderr.write("%4d/%02d/%02d %02d:%02d %s\n" %
                     (now.tm_year, now.tm_mon, now.tm_mday,
                      now.tm_hour, now.tm_min, msg))
    sys.stderr.flush()

def crit(msg):
    '''Print critical message to log'''
    logit("CRI: " + msg)

def err(msg):
    '''Print error message to log'''
    logit("ERR: " + msg)

def warn(msg):
    '''Print warning message to log'''
    logit("WRN: " + msg)

def ssh_cmd(host):
    '''Return ssh command line for host. Empty host imples local execution.'''
    if host != '':
        return SSH_CMD % (TEGU_USER, host)
    return ''

def get_checkpoint(host=''):
    '''Fetches checkpoint files from specified host.'''
    ssh_prefix = ssh_cmd(host)

    try:
        subprocess.check_call(ssh_prefix + SYNC_CMD, shell="True")
        return True
    except subprocess.CalledProcessError:
        warn("Could not sync chkpts from %s" % host)
    return False

def my_aliases():
    '''
        returns a map (dictionary?) of alias names. if map[name] == true then
        name is an alias for this host.

        this works if hostname returns foo and aliases are some 'extension'
        of foo (e.g. foo-ops), but if foo-ops is returned by host name
        we won't find foo.
    '''

    p = subprocess.Popen( ["hostname", "-f"], stdout=subprocess.PIPE, shell=False )
    sout = p.communicate()[0]
    toks = str.split( str.split( sout, "\n" )[0], ".", 1 ) # split host and domain

    map = {}
    if len( toks ) == 1:    # odd case where -f returns just foo and not foo.domain
        map[toks[0]] = True
    else:
        p1 = subprocess.Popen( ["dig", toks[1], "axfr"], stdout=subprocess.PIPE )
        p2 = subprocess.Popen( ["sed", "-r", "/^" + toks[0] + "[^a-zA-Z]/! d; s/. .*//"],
            stdin=p1.stdout, stdout=subprocess.PIPE )
        for a in str.split( p2.communicate()[0], "\n" ):
            if not a == None:
                map[a] = True

    return map


def extract_dt(dt_str, dt_col, tm_col):
    '''
        Given a string assumed to be from a long ls listing or
        from a verbose tar output, extract the date and time
        and return the unix timestamp. If the time/date components
        are not recognisable, then returns 0.
    '''
    # prevent stack dump if missing time, or wrong format
    try:
        toks = string.split(dt_str)
        # ls listing has decimal which python %S cannot handle
        ttoks = string.split(toks[tm_col], ".")
        # build time object
        tobj = time.strptime(toks[dt_col] + " " + ttoks[0], "%Y-%m-%d %H:%M:%S")
        return int(time.mktime(tobj))
    except (ValueError, IndexError):
        logit("unable to build a timestamp from: %s and %s"
              % (toks[dt_col], ttoks[0]))
    return 0

def should_be_active(host):
    '''Returns True if host should be active as opposed to current node'''

    # need short name for ls command
    htoks = string.split(host, ".")

    # Cmd to suss out the most recent checkpoint file that was in the
    # most recent tar from host.  Sort to ensure order by date from
    # tar output then take first. Redirect stderr to null because
    # python doesn't ignore closed pipe signals and thus they
    # propagate causing sort and grep to complain once for each line
    # of output it tries to write after head closes the pipe.
    ts_r_cmd = 'tar -t -v --full-time -f $(ls -t ' + LIBDIR + '/chkpt_synch.'\
        + htoks[0] + '.*.tgz | head -1) | (grep resmgr_ | sort -r -k 4,5 '\
        + '| head -1) 2>/dev/null'

    # cmd to suss out the long listing of the most recent of our
    # checkpoint files. Specifically look for resmgr_ files as there
    # might be other files in the directory. -F and grep -v might be
    # overkill, but aren't harmful.
    ts_l_cmd = 'ls --full-time -tF ' + CKPTDIR + '/resmgr_* '\
        + '| grep -v "\\/\\$" 2>/dev/null' + '| head -1'

    # Pull latest checkpoint from remote node
    # If theres an error, the other guy shouldn't be primary
    if not get_checkpoint(host):
        return False
    if not get_checkpoint():
        return True

    # Check if we or they have latest checkpoint
    try:
        # Get clock skew
        logit("checking clock skew: " + ssh_cmd(host) + 'date +%s')

        # I don't grok why shell=True is needed, but it fails without
        time_r = int(subprocess.check_output(ssh_cmd(host) + '/bin/date +%s',
                                             shell=True))
        time_l = int(subprocess.check_output(ssh_cmd('') + 'date +%s',
                                             shell=True))
        skew = time_l-time_r

        # Get ours and their checkpoint file info
        # get ls listing info string
        ts_r_s = subprocess.check_output(ts_r_cmd, shell=True)
        ts_l_s = subprocess.check_output(ts_l_cmd, shell=True)

        if ts_r_s == "" or ts_l_s == "":
            logit("unable to find chkpt file info host:" + host)
            return False

        # convert listing strings in numeric timestamps
        ts_r = extract_dt(ts_r_s, 3, 4)
        ts_l = extract_dt(ts_l_s, 5, 6)

        return ts_r+skew > ts_l or \
            (ts_r+skew == ts_l and host < socket.getfqdn())

    except subprocess.CalledProcessError:
        warn("Could not get chkpt timestamps")

    return False

def is_active(host='localhost'):
    '''Return True if tegu is running on host
       If host is None, check if tegu is running on current host
       Use ping API check, standby file may be inconsistent'''

    # must use no-proxy to avoid proxy servers gumming up the works
    # grep stderr redirected to avoid pipe complaints
    curl_str = ('curl --noproxy \'*\' --connect-timeout %d -s -d '\
                + '"ping" %s://%s:%d/tegu/api | grep -q -i pong 2>/dev/null')\
                % (CONNECT_TIMEOUT, TEGU_PROTO, host, TEGU_PORT)
    for i in xrange(RETRY_COUNT):
        try:
            subprocess.check_call(curl_str, shell=True)
            return True
        except subprocess.CalledProcessError:
            continue
    return False

def deactivate_tegu(host=''):
    ''' Deactivate tegu on a given host. If host is omitted, local
        tegu is stopped. Returns True if successful, False on error.'''
    ssh_prefix = ssh_cmd(host)
    try:
        subprocess.check_call(ssh_prefix + DEACTIVATE_CMD, shell=True)
        return True
    except subprocess.CalledProcessError:
        return False

def activate_tegu(host=''):
    ''' Activate tegu on a given host. If host is omitted, local
        tegu is started. Returns True if successful, False on error.'''
    if host != '':
        host = SSH_CMD % (TEGU_USER, host)
    try:
        subprocess.check_call(host + ACTIVATE_CMD, shell=True)
        return True
    except subprocess.CalledProcessError:
        return False

def main_loop(standby_list, this_node, priority):
    '''Main heartbeat and liveness check loop'''
    priority_wait = False
    while True:
        if not priority_wait:
            # Normal heartbeat
            time.sleep(HEARTBEAT_SEC)
        else:
            # No tegu running. Wait for higher priority tegu to activate.
            time.sleep(PRI_WAIT_SEC*priority)

        i_am_active = is_active()
        any_active = i_am_active

        # If I'm not active, then remove orphaned standby files
        if not i_am_active:
            deactivate_tegu()

        # Check for active tegus
        for host in standby_list:
            if host == this_node:
                continue

            host_active = is_active(host)

            # Check for split brain: 2 tegus active
            if i_am_active and host_active:
                logit("checking for split")
                host_active = should_be_active(host)
                if host_active:
                    logit("deactivate myself, " + host + " already running")
                    deactivate_tegu()      # Deactivate myself
                    i_am_active = False
                else:
                    logit("deactivate " + host + " already running here")
                    deactivate_tegu(host)  # Deactivate other tegu

            # Track that at-least one tegu is active
            any_active = any_active or host_active

        # If no active tegu, then we must try to start one
        if not any_active:
            if priority_wait or priority == 0:
                logit("no running tegu found, starting here")
                priority_wait = False
                activate_tegu()            # Start local tegu
            else:
                priority_wait = True
    # end loop

def main():
    '''Main function'''

    logit("tegu_ha v1.0 started")


    ok = False
    mcount = 0                  # critical error after an hour of waiting
    while not ok:               # loop until we find us
        alias_map = my_aliases()    # generate each time round in case dns changes

        ok = False
        # Read list of standby tegu nodes and find us
        standby_list = [l.strip() for l in open(STDBY_LIST, 'r')]

        for i in range( len( standby_list ) ):
            if alias_map.has_key( standby_list[i] ):
                priority = i
                ok = True
                this_node = standby_list[i]
                break

        if not ok:
            if mcount == 0:         # dont flood the log
                logit("Could not find this host in standby list: %s (waiting)" % STDBY_LIST)
                logit( "aliases for this host: %s" % alias_map )
            else:
                if mcount == 60:
                    crit("Could not find this host in standby list: %s" % STDBY_LIST)
                    mcount = 0      # another message in about an hour
            mcount += 1
            time.sleep( 60 )

    logit( "host matched in standby list: %s priority=%d" % (standby_list[priority], priority) )

    # Loop forever listening to heartbeats
    main_loop(standby_list, this_node, priority)


if __name__ == '__main__'  or  __name__ == "main":
    main()
