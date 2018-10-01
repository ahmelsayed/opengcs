package fs

import (
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// Ext4Fs implements the Filesystem interface for ext4.
//
// Ext4Fs makes the following assumptions about the ext4 file system.
//  - No journal or GDT table
//	- Extent tree (instead of direct/indirect block addressing)
//	- Hash tree directories (instead of linear directories)
//	- Inline symlinks if < 60 chars, but no inline directories or reg files
//  - sparse_super ext4 flags, so superblocks backups are in powers of 3, 5, 7
// 	- Directory entries take 1 block (even though its not true)
//  - All regular files/symlinks <= 128MB
type Ext4Fs struct {
	BlockSize uint64
	InodeSize uint64
	totalSize uint64
	numInodes uint64
}

// InitSizeContext creates the context for a new ext4 filesystem context
// Before calling set e.BlockSize and e.InodeSize to the desired values.
func (e *Ext4Fs) InitSizeContext() error {
	e.numInodes = 11                                                // ext4 has 11 reserved inodes
	e.totalSize = maxU64(2048+e.numInodes*e.InodeSize, e.BlockSize) // boot sector + super block is 2k

	logrus.Infof("InitSizeContext: %+v", e)

	return nil
}

// CalcRegFileSize calculates the space taken by the given regular file on a ext4
// file system with extent trees.
func (e *Ext4Fs) CalcRegFileSize(fileName string, fileSize uint64) error {
	// 1 directory entry
	// 1 inode
	e.addInode()
	e.totalSize += e.BlockSize

	// Each extent can hold 32k blocks, so 32M of data, so 128MB can get held
	// in the 4 extends below the i_block.
	e.totalSize += alignN(fileSize, e.BlockSize)
	logrus.Infof("CalcRegFileSize: %+v %s %d", e, fileName, fileSize)
	return nil
}

// CalcDirSize calculates the space taken by the given directory on a ext4
// file system with hash tree directories enabled.
func (e *Ext4Fs) CalcDirSize(dirName string) error {
	// 1 directory entry for parent.
	// 1 inode with 2 directory entries ("." & ".." as data
	e.addInode()
	e.totalSize += 3 * e.BlockSize
	logrus.Infof("CalcDirSize: %+v %s", e, dirName)
	return nil
}

// CalcSymlinkSize calculates the space taken by a symlink taking account for
// inline symlinks.
func (e *Ext4Fs) CalcSymlinkSize(srcName string, dstName string) error {
	e.addInode()
	if len(dstName) > 60 {
		// Not an inline symlink. The path is 1 extent max since MAX_PATH=4096
		e.totalSize += alignN(uint64(len(dstName)), e.BlockSize)
	}
	logrus.Infof("CalcSynlinkSize: %+v %s %s", e, srcName, dstName)
	return nil
}

// CalcHardlinkSize calculates the space taken by a hardlink.
func (e *Ext4Fs) CalcHardlinkSize(srcName string, dstName string) error {
	// 1 directory entry (No additional inode)
	e.totalSize += e.BlockSize
	logrus.Infof("CalcHardlinkSize: %+v %s %s", e, srcName, dstName)
	return nil
}

// CalcCharDeviceSize calculates the space taken by a char device.
func (e *Ext4Fs) CalcCharDeviceSize(devName string, major uint64, minor uint64) error {
	e.addInode()
	logrus.Infof("CalcCharDeviceSize: %+v %s %d %d", e, devName, major, minor)
	return nil
}

// CalcBlockDeviceSize calculates the space taken by a block device.
func (e *Ext4Fs) CalcBlockDeviceSize(devName string, major uint64, minor uint64) error {
	e.addInode()
	logrus.Infof("CalcBlockDeviceSize: %+v %s %d %d", e, devName, major, minor)
	return nil
}

// CalcFIFOPipeSize calculates the space taken by a fifo pipe.
func (e *Ext4Fs) CalcFIFOPipeSize(pipeName string) error {
	logrus.Infof("CalcFIFOPipeSize: %+v %s", e, pipeName)
	e.addInode()
	return nil
}

// CalcSocketSize calculates the space taken by a socket.
func (e *Ext4Fs) CalcSocketSize(sockName string) error {
	logrus.Infof("CalcSocketSize: %+v %s", e, sockName)
	e.addInode()
	return nil
}

// CalcAddExAttrSize calculates the space taken by extended attributes.
func (e *Ext4Fs) CalcAddExAttrSize(fileName string, attr string, data []byte, flags int) error {
	// Since xattr are stored in the inode, we don't use any more space
	logrus.Infof("CalcAddExAttrSize: %+v %s", e, fileName)
	return nil
}

// FinalizeSizeContext should be after all of the CalcXSize methods are done.
// It does some final size adjustments.
func (e *Ext4Fs) FinalizeSizeContext() error {
	logrus.Infof("FinalizeSizeContext Entry: %+v", e)
	// Final adjustments to the size + inode
	// There are more metadata like Inode Table, block table.
	// For now, add 15% more to the size to take account for it, and
	// 10% more inodes. See https://github.com/moby/moby/issues/36353
	e.totalSize = uint64(float64(e.totalSize) * 1.50)
	e.numInodes = uint64(float64(e.numInodes) * 1.50)

	// Align to 64 * blocksize
	if e.totalSize%(64*e.BlockSize) != 0 {
		e.totalSize = alignN(e.totalSize, 64*e.BlockSize)
	}
	logrus.Infof("FinalizeSizeContext Exit: %+v", e)
	return nil
}

// GetSizeInfo returns the size of the ext4 file system after the size context is finalized.
func (e *Ext4Fs) GetSizeInfo() FilesystemSizeInfo {
	return FilesystemSizeInfo{NumInodes: e.numInodes, TotalSize: e.totalSize}
}

// CleanupSizeContext frees any resources needed by the ext4 file system
func (e *Ext4Fs) CleanupSizeContext() error {
	// No resources need to be freed
	return nil
}

// MakeFileSystem writes an ext4 filesystem to the given file after the size context is finalized.
func (e *Ext4Fs) MakeFileSystem(file *os.File) error {
	blockSize := strconv.FormatUint(e.BlockSize, 10)
	inodeSize := strconv.FormatUint(e.InodeSize, 10)
	numInodes := strconv.FormatUint(e.numInodes, 10)
	logrus.Infof("making file system with: bs=%d is=%d numi=%d size=%d",
		e.BlockSize, e.InodeSize, e.numInodes, e.totalSize)

	err := exec.Command(
		"mkfs.ext4",
		"-O", "^has_journal,^resize_inode",
		"-N", numInodes,
		"-b", blockSize,
		"-I", inodeSize,
		"-F",
		file.Name()).Run()

	if err != nil {
		logrus.Infof("running mkfs.ext4 %s failed with ... (%s)", file.Name(), err)
		time.Sleep(100 * time.Hour)
	}

	return err
}

// MakeBasicFileSystem just creates an empty file system on the given file using
// the default settings.
func (e *Ext4Fs) MakeBasicFileSystem(file *os.File) error {
	return exec.Command("mkfs.ext4", "-F", file.Name()).Run()
}

func maxU64(x, y uint64) uint64 {
	if x > y {
		return x
	}
	return y
}

func alignN(n uint64, alignto uint64) uint64 {
	if n%alignto == 0 {
		return n
	}
	return n + alignto - n%alignto
}

func (e *Ext4Fs) addInode() {
	e.numInodes++
	e.totalSize += e.InodeSize
}
