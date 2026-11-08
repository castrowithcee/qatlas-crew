// Package project holds the projects qcrew works in and the configuration file that lists them.
//
// A project is a directory registered by its path; nothing inside it is required or changed. Its id is a
// random, stable 12-character base32 word; its name defaults to the base name of the directory and is unique,
// and neither may equal the other of another project, so a reference by id or name is never ambiguous. The
// path is stored absolute with symbolic links resolved.
//
// The configuration file is YAML, meant to be edited by hand, and read strictly: it must carry the schema
// version this package knows, and an unknown key is an error instead of being dropped. Registry changes it
// only through a configfile.Store, under its cross-process lock and with a backup of the replaced bytes;
// within qcrew only the service calls it.
package project
