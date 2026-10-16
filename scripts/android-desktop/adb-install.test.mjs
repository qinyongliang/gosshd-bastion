import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, readFileSync, existsSync, rmSync, chmodSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname, basename, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

function install({ devices = 'example-device-a\tdevice', sdk = '35', enable = false, serial, fail = false } = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'gosshd-adb-fixture-'));
  try {
    const apk = join(dir, 'example agent.apk');
    const log = join(dir, 'calls.jsonl');
    const fake = join(dir, 'fake-adb.mjs');
    writeFileSync(apk, 'public APK fixture');
    writeFileSync(fake, String.raw`import {appendFileSync} from 'node:fs';
const args=process.argv.slice(2);
appendFileSync(process.env.FAKE_ADB_LOG,JSON.stringify(args)+'\n');
if(args[0]==='devices') console.log('List of devices attached\n'+process.env.FAKE_ADB_DEVICES);
else if(args.includes('getprop')) console.log(process.env.FAKE_ADB_SDK);
else if(args.includes('install') && process.env.FAKE_ADB_FAIL==='1') { console.error('fixture install failure'); process.exit(7); }
else console.log('Success');
`);
    const windows = process.platform === 'win32';
    const adb = join(dir, windows ? 'fake-adb.cmd' : 'fake-adb');
    const node = process.execPath;
    writeFileSync(adb, windows
      ? '@echo off\r\n"'+node+'" "'+fake+'" %*\r\nexit /b %errorlevel%\r\n'
      : '#!/bin/sh\nexec "'+node+'" "'+fake+'" "$@"\n');
    if (!windows) chmodSync(adb, 0o700);
    const env = { ...process.env, FAKE_ADB_LOG: log, FAKE_ADB_DEVICES: devices, FAKE_ADB_SDK: sdk, FAKE_ADB_FAIL: fail ? '1' : '0' };
    let command, args;
    if (windows) {
      command = join(process.env.SystemRoot, 'System32/WindowsPowerShell/v1.0/powershell.exe');
      env.PSModulePath = join(process.env.SystemRoot, 'System32/WindowsPowerShell/v1.0/Modules');
      args = ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', fileURLToPath(new URL('./install-adb.ps1', import.meta.url)), '-ApkPath', apk, '-AdbPath', adb];
      if (serial) args.push('-Serial', serial);
      if (enable) args.push('-EnableAccessibility');
    } else {
      command = 'sh';
      args = [fileURLToPath(new URL('./install-adb.sh', import.meta.url)), '--apk', apk, '--adb', adb];
      if (serial) args.push('--serial', serial);
      if (enable) args.push('--enable-accessibility');
    }
    const result = spawnSync(command, args, { env, encoding: 'utf8', timeout: 20000 });
    if (result.error) throw result.error;
    const calls = existsSync(log) ? readFileSync(log, 'utf8').trim().split('\n').filter(Boolean).map(JSON.parse) : [];
    return { result, calls, apk };
  } finally {
    // Only remove the exact temporary fixture directory created above.
    assert.equal(dirname(resolve(dir)), resolve(tmpdir()));
    assert.ok(basename(dir).startsWith('gosshd-adb-fixture-'));
    rmSync(dir, { recursive: true, force: true });
  }
}

test('install and open the same APK without configuring or elevating Agent', () => {
  const {result,calls,apk}=install();
  assert.equal(result.status,0,result.stderr+result.stdout);
  assert.deepEqual(calls.find(c=>c.includes('install')),['-s','example-device-a','install','-r',apk]);
  assert.ok(calls.some(c=>c.includes('am')));
  assert.ok(!calls.some(c=>c.includes('pm') || c.includes('settings') || c.includes('start-foreground-service')));
});
test('accessibility grant is explicit opt-in',()=>{
  const {result,calls}=install({enable:true});
  assert.equal(result.status,0,result.stderr+result.stdout);
  assert.ok(calls.some(c=>c.includes('grant') && c.includes('android.permission.WRITE_SECURE_SETTINGS')));
});
for(const [name,options]of Object.entries({missing:{devices:''},unauthorized:{devices:'example-device-a\tunauthorized'},multiple:{devices:'example-device-a\tdevice\nexample-device-b\tdevice'},oldAndroid:{sdk:'29'}})){
  test('reject '+name+' before installing',()=>{
    const {result,calls}=install(options);
    assert.notEqual(result.status,0);
    assert.ok(calls.some(c=>c[0]==='devices'));
    assert.ok(!calls.some(c=>c.includes('install')));
  });
}
test('explicit device selection is passed to every mutating command',()=>{
  const {result,calls}=install({devices:'example-device-a\tdevice\nexample-device-b\tdevice',serial:'example-device-b'});
  assert.equal(result.status,0,result.stderr+result.stdout);
  assert.ok(calls.filter(c=>c[0]==='-s').every(c=>c[1]==='example-device-b'));
});
test('failed APK upgrade never uninstalls the existing app or launches it',()=>{
  const {result,calls}=install({fail:true});
  assert.notEqual(result.status,0);
  assert.ok(calls.some(c=>c.includes('install')));
  assert.ok(!calls.some(c=>c.includes('uninstall') || c.includes('am')));
});
