import styles from "../page.module.css";

export default function AuthBrand() {
  return (
    <div className={styles.signInBrand}>
      <img
        className={styles.signInLogo}
        src="/dynamic-pdb-mark.svg"
        alt=""
        width={64}
        height={64}
      />
      <span className={styles.signInWordmark}>dynamic-pdb</span>
    </div>
  );
}
