// Package routes names the frontend paths the server has to know about. They
// must match web/src/routes.ts; a test in this package checks that they do.
package routes

// Admin is the admin app's path. Changing it also moves the admin web manifest
// and the links in admin notifications; an icon already on a home screen keeps
// the old start address and has to be added again.
//
// It is not a secret: it ships in the public JavaScript bundle. Leaving the
// usual "/admin" only spares the sign-in form from scanners that probe it.
const Admin = "/tagrolka"

// AdminManifest is the admin app's web manifest.
const AdminManifest = Admin + ".webmanifest"
