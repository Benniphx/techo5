/* Restart with the reboot-mode "normal" command, so the Rockchip boot mode register says normal. */
#include <linux/reboot.h>
#include <sys/syscall.h>
#include <unistd.h>

int main(int argc, char **argv)
{
	const char *mode = argc > 1 ? argv[1] : "normal";

	sync();
	return syscall(SYS_reboot, LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2,
		       LINUX_REBOOT_CMD_RESTART2, mode);
}
