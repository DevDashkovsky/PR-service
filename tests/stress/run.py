#!/usr/bin/env python3
"""Local load and failure checks; creates its own PostgreSQL and race-enabled API.

Requires Go, PostgreSQL 16 tools and Python 3.9+. Never targets an existing DB.
Artifacts and logs are kept in a new temporary directory printed at startup.
"""
import argparse
import tempfile
import shutil
import concurrent.futures as cf
import collections
import json
import os
import pathlib
import signal
import socket
import subprocess
import time
import urllib.request
import urllib.error
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--requests', type=int, default=500)
parser.add_argument('--concurrency', type=int, default=32)
parser.add_argument('--duration', type=int, default=30, help='seconds of sustained mixed traffic (0 disables)')
args = parser.parse_args()
if not 1 <= args.requests <= 10000 or not 1 <= args.concurrency <= 128:
    parser.error('requests must be 1..10000; concurrency must be 1..128')
if not 0 <= args.duration <= 300:
    parser.error('duration must be 0..300 seconds')
for tool in ('go', 'initdb', 'pg_ctl', 'psql'):
    if not shutil.which(tool):
        parser.error('missing tool: ' + tool)
project = pathlib.Path(__file__).resolve().parents[2]
os.chdir(project)
root = pathlib.Path(tempfile.mkdtemp(prefix='pr-service-resilience-', dir='/tmp'))
(root / 'socket').mkdir()
print('Artifacts: ' + str(root), flush=True)
print('Requests per phase: '+str(args.requests)+'; concurrency: '+str(args.concurrency), flush=True)
dsn = 'postgres://review@/postgres?host=' + str(root / 'socket') + '&sslmode=disable'
base = ''
server = None
results = []
failures = []

def sql(q):
    return subprocess.run(['psql', dsn, '-XAt', '-v', 'ON_ERROR_STOP=1', '-c', q], capture_output=True, text=True, check=True).stdout.strip()

def call(method, path, payload=None):
    start = time.monotonic()
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(base + path, data=data, method=method, headers={'Content-Type': 'application/json'})
    try:
        try:
            response = urllib.request.urlopen(req, timeout=35)
        except urllib.error.HTTPError as e:
            response = e
        with response:
            status = response.status
            body = response.read().decode()
        try:
            body = json.loads(body)
        except ValueError:
            pass
        return (status, body, time.monotonic() - start, dict(response.headers))
    except Exception as e:
        return (0, str(e), time.monotonic() - start, {})

def record(name, ok, **details):
    row = {'name': name, 'passed': bool(ok), **details}
    results.append(row)
    if not ok:
        failures.append(name)
    print(json.dumps(row, ensure_ascii=False), flush=True)

def wait_sql(q, timeout=5):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        result = sql(q)
        if result:
            return result
        time.sleep(0.05)
    raise RuntimeError('SQL condition timed out: ' + q)

def runload(name, jobs, expected):
    start = time.monotonic()
    with cf.ThreadPoolExecutor(max_workers=args.concurrency) as ex:
        rows = list(ex.map(lambda job: call(*job), jobs))
    elapsed = time.monotonic() - start
    summarize(name, rows, elapsed, expected)
    return rows

def summarize(name, rows, elapsed, expected):
    counts = collections.Counter(r[0] for r in rows)
    lat = sorted(r[2] * 1000 for r in rows)
    record(
        name, all(r[0] in expected for r in rows),
        requests=len(rows), statuses=dict(counts), seconds=round(elapsed, 3),
        rps=round(len(rows) / elapsed, 1),
        p50_ms=round(lat[int(0.5 * (len(lat) - 1))], 2),
        p95_ms=round(lat[int(0.95 * (len(lat) - 1))], 2),
        p99_ms=round(lat[int(0.99 * (len(lat) - 1))], 2),
    )

def sustained_load():
    if not args.duration:
        return
    print('Starting sustained mixed traffic for '+str(args.duration)+' seconds', flush=True)
    start = time.monotonic()
    deadline = start + args.duration

    def worker(number):
        rows = []
        sequence = 0
        while time.monotonic() < deadline:
            identifier = f'steady-{number}-{sequence}'
            rows.append(call('POST', '/pullRequest/create', {
                'pull_request_id': identifier, 'pull_request_name': 'Steady load',
                'author_id': 'u0',
            }))
            rows.append(call('GET', '/users/getReview?user_id=u'+str(number % 31 + 1)))
            rows.append(call('POST', '/pullRequest/merge', {'pull_request_id': identifier}))
            sequence += 1
        return rows

    with cf.ThreadPoolExecutor(max_workers=args.concurrency) as executor:
        groups = list(executor.map(worker, range(args.concurrency)))
    rows = [row for group in groups for row in group]
    summarize('sustained create/read/merge', rows, time.monotonic() - start, {200, 201})

def lock_prs():
    env = os.environ.copy()
    env['PGAPPNAME'] = 'stress-lock'
    p = subprocess.Popen(['psql', dsn, '-XAt', '-v', 'ON_ERROR_STOP=1', '-c', 'BEGIN; LOCK TABLE pull_requests IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(60); ROLLBACK;'], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    pid = wait_sql("SELECT pid FROM pg_stat_activity WHERE application_name='stress-lock' AND wait_event='PgSleep'")
    return (p, pid)

def release(lock):
    p, pid = lock
    sql('SELECT pg_terminate_backend(' + pid + ')')
    p.wait(timeout=5)

def main():
    global base, server
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        port = s.getsockname()[1]
    base = 'http://127.0.0.1:' + str(port)
    env = os.environ.copy()
    env.update(DATABASE_URL=dsn + '&pool_max_conns=16&application_name=stress-api', ENVIRONMENT='development', PORT=str(port))
    with (root / 'api.log').open('w') as log:
        server = subprocess.Popen([str(root / 'server')], env=env, stdout=log, stderr=log)
        try:
            end = time.monotonic() + 10
            while time.monotonic() < end:
                if server.poll() is not None:
                    raise RuntimeError('API failed to start')
                if call('GET', '/readyz')[0] == 200:
                    break
                time.sleep(0.05)
            else:
                raise RuntimeError('Readiness timeout')
            test_env = os.environ.copy()
            test_env['PR_SERVICE_E2E_URL'] = base
            subprocess.run(['go', 'test', '-race', '-count=1', './tests/e2e'], env=test_env, check=True)
            members = [{'user_id': 'u' + str(i), 'username': 'User ' + str(i), 'is_active': True} for i in range(32)]
            assert call('POST', '/team/add', {'team_name': 'stress', 'members': members})[0] == 201
            n = args.requests
            create = lambda i: ('POST', '/pullRequest/create', {'pull_request_id': 'load-' + str(i), 'pull_request_name': 'Load', 'author_id': 'u0'})
            runload('create distinct PRs', [create(i) for i in range(n)], {201})
            runload('read reviews', [('GET', '/users/getReview?user_id=u' + str(i % 31 + 1)) for i in range(n)], {200})
            runload('merge distinct PRs', [('POST', '/pullRequest/merge', {'pull_request_id': 'load-' + str(i)}) for i in range(n)], {200})
            same = runload('concurrent duplicate create', [create('duplicate')] * 64, {201, 409})
            record('exactly one duplicate create succeeds', sum((r[0] == 201 for r in same)) == 1)
            r = call(*create('reassign'))
            old = r[1]['pr']['assigned_reviewers'][0]
            same = runload('concurrent replacement of one reviewer', [('POST', '/pullRequest/reassign', {'pull_request_id': 'load-reassign', 'old_user_id': old})] * 64, {200, 409})
            record('exactly one replacement succeeds', sum((r[0] == 200 for r in same)) == 1)
            merged = runload('concurrent idempotent merge', [('POST', '/pullRequest/merge', {'pull_request_id': 'load-reassign'})] * 64, {200})
            record('merge timestamp remains stable', len({r[1]['pr']['mergedAt'] for r in merged if r[0] == 200}) == 1)
            sustained_load()
            jobs = []
            for i in range(300):
                jobs.extend([create('churn-' + str(i)), ('POST', '/users/setIsActive', {'user_id': 'u' + str(i % 31 + 1), 'is_active': i % 2 == 0})])
            churn = runload('create while activity changes', jobs, {200, 201, 503})
            busy = [(job, row) for job, row in zip(jobs, churn) if row[0] == 503]
            record('contention response is explicitly retryable', all((row[1].get('error', {}).get('code') == 'SERVICE_UNAVAILABLE' and row[3].get('Retry-After') == '1' for _, row in busy)), responses=len(busy))
            if busy:
                time.sleep(1)  # Honor Retry-After before resubmitting rolled-back work.
                runload('retry after contention', [job for job, _ in busy], {200, 201})
            record('persistent assignment invariants', sql("SELECT (SELECT count(*) FROM pr_reviewers r JOIN pull_requests p USING(pull_request_id) WHERE r.reviewer_id=p.author_id)+(SELECT count(*) FROM (SELECT pull_request_id FROM pr_reviewers GROUP BY pull_request_id HAVING count(*)>2) x)+(SELECT count(*) FROM pull_requests WHERE status='MERGED' AND merged_at IS NULL)") == '0')
            lock = lock_prs()
            try:
                r = call('POST', '/pullRequest/merge', {'pull_request_id': 'load-0'})
                record('database lock request deadline', r[0] == 504 and isinstance(r[1], dict) and (r[1].get('error', {}).get('code') == 'GATEWAY_TIMEOUT'), status=r[0], body=r[1], seconds=round(r[2], 3))
            finally:
                release(lock)
            sql('CREATE FUNCTION stress_delay() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(60); RETURN NEW; END $$; CREATE TRIGGER stress_delay BEFORE INSERT ON pr_reviewers FOR EACH ROW EXECUTE FUNCTION stress_delay()')
            try:
                with cf.ThreadPoolExecutor(max_workers=1) as ex:
                    pending = ex.submit(call, *create('interrupted'))
                    pid = wait_sql("SELECT pid FROM pg_stat_activity WHERE application_name='stress-api' AND wait_event='PgSleep'")
                    sql('SELECT pg_terminate_backend(' + pid + ')')
                    r = pending.result(timeout=5)
                    count = sql("SELECT (SELECT count(*) FROM pull_requests WHERE pull_request_id='load-interrupted')+(SELECT count(*) FROM pr_reviewers WHERE pull_request_id='load-interrupted')")
                    record('disconnect rolls back partial creation', r[0] >= 500 and count == '0', status=r[0], rows=count)
            finally:
                sql('DROP TRIGGER stress_delay ON pr_reviewers; DROP FUNCTION stress_delay()')
            r = call(*create('interrupted'))
            record('retry after rolled-back creation', r[0] == 201, status=r[0])
            subprocess.run(['pg_ctl', '-D', str(root / 'data'), '-m', 'immediate', '-w', 'stop'], check=True, capture_output=True)
            try:
                health = call('GET', '/healthz')
                ready = call('GET', '/readyz')
                read = call('GET', '/users/getReview?user_id=u1')
                record('database outage detection', health[0] == 200 and ready[0] == 503 and (read[0] >= 500), health=health[0], ready=ready[0], read=read[0], read_seconds=round(read[2], 3))
            finally:
                subprocess.run(['pg_ctl', '-D', str(root / 'data'), '-l', str(root / 'postgres.log'), '-o', "-h '' -k " + str(root / 'socket'), '-w', 'start'], check=True, capture_output=True)
            ready = call('GET', '/readyz')
            r = call(*create('recovery'))
            record('recovery without API restart', ready[0] == 200 and r[0] == 201, ready=ready[0], create=r[0])
            lock = lock_prs()
            try:
                with cf.ThreadPoolExecutor(max_workers=1) as ex:
                    pending = ex.submit(call, 'POST', '/pullRequest/merge', {'pull_request_id': 'load-recovery'})
                    wait_sql("SELECT pid FROM pg_stat_activity WHERE application_name='stress-api' AND wait_event_type='Lock'")
                    started = time.monotonic()
                    server.send_signal(signal.SIGTERM)
                    deadline = time.monotonic() + 5
                    while 'Получен сигнал завершения' not in (root / 'api.log').read_text() and time.monotonic() < deadline:
                        time.sleep(0.02)
                    release(lock)
                    lock = None
                    r = pending.result(timeout=15)
                    server.wait(timeout=15)
                    record('graceful shutdown drains in-flight request', r[0] == 200 and server.returncode == 0, status=r[0], exit=server.returncode, seconds=round(time.monotonic() - started, 3))
            finally:
                if lock:
                    release(lock)
        finally:
            if server.poll() is None:
                server.terminate()
                try:
                    server.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()
        record('race detector', 'WARNING: DATA RACE' not in (root / 'api.log').read_text())
        (root / 'results.json').write_text(json.dumps(results, indent=2, ensure_ascii=False) + '\n')
        print('FAILURES: ' + repr(failures), flush=True)
try:
    with (root / 'build.log').open('w') as log:
        subprocess.run(['go', 'build', '-race', '-o', str(root / 'server'), './cmd/server'], check=True, stdout=log, stderr=log)
    with (root / 'init.log').open('w') as log:
        subprocess.run(['initdb', '-D', str(root / 'data'), '-A', 'trust', '-U', 'review', '--no-locale', '-E', 'UTF8'], check=True, stdout=log, stderr=log)
    subprocess.run(['pg_ctl', '-D', str(root / 'data'), '-l', str(root / 'postgres.log'), '-o', "-h '' -k " + str(root / 'socket'), '-w', 'start'], check=True, capture_output=True)
    main()
finally:
    if (root / 'data' / 'postmaster.pid').exists():
        subprocess.run(['pg_ctl', '-D', str(root / 'data'), '-m', 'fast', '-w', 'stop'], check=True, capture_output=True)
    (root / 'results.json').write_text(json.dumps(results, indent=2, ensure_ascii=False) + '\n')
raise SystemExit(1 if failures else 0)
