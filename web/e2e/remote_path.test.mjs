import assert from "node:assert/strict";
import test from "node:test";
import { normalizeRemotePath, remoteJoin, remoteParent } from "../src/remotePath.ts";

test("Windows native and SFTP paths use one absolute protocol spelling", () => {
 for (const path of ["/D:/3D/灯.scad", "D:/3D/灯.scad", "d:\\3D\\灯.scad"]) {
  assert.equal(normalizeRemotePath(path), "/D:/3D/灯.scad");
 }
 assert.equal(normalizeRemotePath("/C:"), "/C:/");
 assert.equal(remoteJoin("/D:/3D", "新灯.scad"), "/D:/3D/新灯.scad");
 assert.equal(remoteJoin("/C:/Users/user", "D:\\3D"), "/D:/3D");
 assert.equal(remoteParent("/D:/3D"), "/D:/");
 assert.equal(remoteParent("D:/"), "/D:/");
});

test("POSIX and UNC paths retain their filesystem roots", () => {
 assert.equal(normalizeRemotePath("/home/user/work/../中文"), "/home/user/中文");
 assert.equal(remoteJoin("/home/user", "../shared"), "/home/shared");
 assert.equal(remoteParent("/home"), "/");
 assert.equal(normalizeRemotePath("."), ".");
 assert.equal(normalizeRemotePath("\\\\server\\share\\中文"), "//server/share/中文");
 assert.equal(remoteParent("//server/share/中文"), "//server/share");
 assert.equal(remoteParent("//server/share"), "//server/share");
 assert.equal(normalizeRemotePath("//server/share/../../"), "//server/share");
});
